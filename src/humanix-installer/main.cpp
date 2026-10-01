#define _DEFAULT_SOURCE
#include <iostream>
#include <vector>
#include <string>
#include <filesystem>
#include <fstream>
#include <sstream>
#include <cstdlib>
#include <csignal>
#include <cctype>
#include <cstdint>
#include <cstring>
#include <unistd.h>
#include <sys/stat.h>
#include <sys/mount.h>
#include <sys/wait.h>

namespace fs = std::filesystem;

struct BlockDevice {
    std::string name;       // sda, nvme0n1
    std::string path;       // /dev/sda
    std::string size_human;
    std::string model;
    uint64_t size_mib = 0;
};

int run_cmd(const std::string& cmd) {
    int ret = std::system(cmd.c_str());
    if (ret == -1 || !WIFEXITED(ret)) return 1;
    return WEXITSTATUS(ret);
}

bool command_exists(const std::string& name) {
    return std::system(("command -v " + name + " >/dev/null 2>&1").c_str()) == 0;
}

void print_step(int current, const std::string& title) {
    std::cout << "\n\033[1;36m[" << current << "/6]\033[0m " << title << std::endl;
}

std::string get_cmd_output(const std::string& cmd) {
    char buffer[256];
    std::string result;
    FILE* pipe = popen(cmd.c_str(), "r");
    if (!pipe) return "";
    while (fgets(buffer, sizeof(buffer), pipe) != nullptr) {
        result += buffer;
    }
    pclose(pipe);
    while (!result.empty() && (result.back() == '\n' || result.back() == '\r')) {
        result.pop_back();
    }
    return result;
}

std::string get_boot_medium_device() {
    std::ifstream mounts("/proc/mounts");
    std::string line;
    while (std::getline(mounts, line)) {
        if (line.find("/run/live/medium") != std::string::npos || line.find("/lib/live/mount/medium") != std::string::npos) {
            std::istringstream iss(line);
            std::string dev;
            iss >> dev;
            std::string base = fs::path(dev).filename().string();
            while (!base.empty() && std::isdigit(base.back())) {
                base.pop_back();
            }
            if (!base.empty() && base.back() == 'p') base.pop_back();
            return base;
        }
    }
    return "";
}

std::vector<BlockDevice> scan_disks() {
    std::vector<BlockDevice> disks;
    std::string live_media = get_boot_medium_device();

    for (const auto& entry : fs::directory_iterator("/sys/block")) {
        std::string name = entry.path().filename().string();

        if (name.rfind("loop", 0) == 0 || name.rfind("ram", 0) == 0 || name.rfind("zram", 0) == 0 || name.rfind("sr", 0) == 0) {
            continue;
        }

        if (!live_media.empty() && name == live_media) {
            continue;
        }

        BlockDevice dev;
        dev.name = name;
        dev.path = "/dev/" + name;

        // Размер
        std::ifstream size_file(entry.path() / "size");
        uint64_t sectors = 0;
        if (size_file >> sectors) {
            double gib = (sectors * 512.0) / (1024.0 * 1024.0 * 1024.0);
            char buf[32];
            std::snprintf(buf, sizeof(buf), "%.2f GiB", gib);
            dev.size_human = buf;
            dev.size_mib = sectors / 2048;
        } else {
            dev.size_human = "Unknown";
        }

        // Модель
        std::ifstream model_file(entry.path() / "device/model");
        std::string model;
        if (std::getline(model_file, model)) {
            dev.model = model;
        } else {
            dev.model = "Generic Storage Device";
        }

        disks.push_back(dev);
    }
    return disks;
}

std::string get_part_name(const std::string& disk_path, int part_num) {
    if (disk_path.find("nvme") != std::string::npos || disk_path.find("mmcblk") != std::string::npos) {
        return disk_path + "p" + std::to_string(part_num);
    }
    return disk_path + std::to_string(part_num);
}


bool read_line(const std::string& prompt, std::string& value) {
    std::cout << prompt << std::flush;
    return static_cast<bool>(std::getline(std::cin, value));
}

bool ask_size(const std::string& prompt, uint64_t& value, uint64_t default_value) {
    std::string input;
    if (!read_line(prompt, input)) return false;
    if (input.empty()) { value = default_value; return true; }
    try {
        size_t used = 0;
        value = std::stoull(input, &used);
        return used == input.size();
    } catch (...) { return false; }
}

bool valid_username(const std::string& value) {
    if (value.empty() || value.size() > 32 ||
        !(std::islower(static_cast<unsigned char>(value[0])) || value[0] == '_')) return false;
    for (unsigned char c : value) {
        if (!(std::islower(c) || std::isdigit(c) || c == '_' || c == '-')) return false;
    }
    return true;
}

bool valid_hostname(const std::string& value) {
    if (value.empty() || value.size() > 63 || value.front() == '-' || value.back() == '-') return false;
    for (unsigned char c : value) {
        if (!(std::isalnum(c) || c == '-')) return false;
    }
    return true;
}

bool read_password(const std::string& prompt, std::string& value) {
    char* first = getpass(prompt.c_str());
    if (!first || !*first) return false;
    std::string password(first);
    char* second = getpass("Confirm password: ");
    if (!second || password != second || password.find_first_of(":\r\n") != std::string::npos) {
        std::cerr << "Passwords did not match or contain an unsupported character.\n";
        return false;
    }
    value = password;
    return true;
}

int run_chroot(const std::string& root, const std::vector<std::string>& args) {
    pid_t pid = fork();
    if (pid < 0) return 1;
    if (pid == 0) {
        std::vector<char*> argv;
        argv.push_back(const_cast<char*>("chroot"));
        argv.push_back(const_cast<char*>(root.c_str()));
        for (const auto& arg : args) argv.push_back(const_cast<char*>(arg.c_str()));
        argv.push_back(nullptr);
        execvp("chroot", argv.data());
        _exit(127);
    }
    int status = 0;
    if (waitpid(pid, &status, 0) < 0 || !WIFEXITED(status)) return 1;
    return WEXITSTATUS(status);
}

bool set_password(const std::string& root, const std::string& account, const std::string& password) {
    int fds[2];
    if (pipe(fds) != 0) return false;
    pid_t pid = fork();
    if (pid < 0) {
        close(fds[0]);
        close(fds[1]);
        return false;
    }
    if (pid == 0) {
        close(fds[1]);
        if (dup2(fds[0], STDIN_FILENO) < 0) _exit(127);
        close(fds[0]);
        execlp("chroot", "chroot", root.c_str(), "chpasswd", static_cast<char*>(nullptr));
        _exit(127);
    }
    close(fds[0]);
    std::string input = account + ":" + password + "\n";
    size_t offset = 0;
    while (offset < input.size()) {
        ssize_t written = write(fds[1], input.data() + offset, input.size() - offset);
        if (written <= 0) break;
        offset += static_cast<size_t>(written);
    }
    close(fds[1]);
    int status = 0;
    if (waitpid(pid, &status, 0) < 0 || offset != input.size() || !WIFEXITED(status)) return false;
    return WEXITSTATUS(status) == 0;
}

int main() {
    signal(SIGPIPE, SIG_IGN);
    if (geteuid() != 0) {
        std::cerr << "\033[1;31m[ERROR] Run humanix-installer with sudo.\033[0m\n";
        return 1;
    }

    std::cout << "\033[1;36m"
              << "==============================================\n"
              << "              HUMANIX INSTALLER               \n"
              << "==============================================\033[0m\n"
              << "Every partition on the selected disk will be erased.\n\n";

    const std::vector<std::string> required = {
        "wipefs", "parted", "udevadm", "mkfs.vfat", "mkfs.ext4", "mkswap",
        "mount", "rsync", "blkid", "chroot", "grub-install", "update-grub"
    };
    std::vector<std::string> missing;
    for (const auto& command : required) {
        if (!command_exists(command)) missing.push_back(command);
    }
    if (!missing.empty()) {
        std::cerr << "\033[1;31mInstaller tools are missing:\033[0m";
        for (const auto& command : missing) std::cerr << " " << command;
        std::cerr << "\nBoot a complete Humanix installer image and try again.\n";
        return 1;
    }

    auto disks = scan_disks();
    if (disks.empty()) {
        std::cerr << "\033[1;31mNo valid target disks found.\033[0m\n";
        return 1;
    }

    std::cout << "\033[1mAvailable disks\033[0m\n";
    for (size_t i = 0; i < disks.size(); ++i) {
        std::cout << "  \033[1;36m" << (i + 1) << ")\033[0m "
                  << disks[i].path << "  " << disks[i].size_human
                  << "  \033[90m" << disks[i].model << "\033[0m\n";
    }
    std::string input;
    if (!read_line("\nChoose disk number: ", input)) return 1;
    size_t choice = 0;
    try {
        size_t used = 0;
        choice = std::stoul(input, &used);
        if (used != input.size() || choice == 0 || choice > disks.size()) throw 1;
    } catch (...) {
        std::cerr << "Invalid disk selection.\n";
        return 1;
    }
    const BlockDevice& target = disks[choice - 1];

    std::string hostname, username;
    if (!read_line("Hostname [humanix]: ", hostname)) return 1;
    if (hostname.empty()) hostname = "humanix";
    if (!valid_hostname(hostname)) {
        std::cerr << "Hostname must be 1-63 letters, digits or hyphens and cannot start or end with '-'.\n";
        return 1;
    }
    if (!read_line("Username [humanix]: ", username)) return 1;
    if (username.empty()) username = "humanix";
    if (!valid_username(username)) {
        std::cerr << "Username must start with a lowercase letter or '_' and contain only lowercase letters, digits, '_' or '-'.\n";
        return 1;
    }

    if (username != "humanix" &&
        run_cmd("getent passwd " + username + " >/dev/null 2>&1") == 0) {
        std::cerr << "That username is reserved by the live system. Choose another.\n";
        return 1;
    }

    std::string root_password, user_password;
    if (!read_password("Root password: ", root_password) ||
        !read_password("User password: ", user_password)) return 1;

    uint64_t efi_mib = 512, root_gib = 0, swap_gib = 0, home_gib = 0;
    std::cout << "\nSizes are whole MiB or GiB values. EFI must be at least 260 MiB.\n"
              << "Root size 0 means use all remaining space after swap and home.\n";
    if (!ask_size("EFI size MiB [512]: ", efi_mib, 512) ||
        !ask_size("Root size GiB [0 = remaining]: ", root_gib, 0) ||
        !ask_size("Swap size GiB [0 = none]: ", swap_gib, 0) ||
        !ask_size("Separate /home size GiB [0 = none]: ", home_gib, 0)) {
        std::cerr << "Enter whole non-negative sizes.\n";
        return 1;
    }

    if (efi_mib < 260 || efi_mib > 4096 || target.size_mib <= efi_mib + 2 ||
        swap_gib > UINT64_MAX / 1024 || home_gib > UINT64_MAX / 1024 ||
        root_gib > UINT64_MAX / 1024) {
        std::cerr << "Disk size or partition size is invalid.\n";
        return 1;
    }
    const uint64_t usable_mib = target.size_mib - efi_mib - 2;
    const uint64_t swap_mib = swap_gib * 1024;
    const uint64_t home_mib = home_gib * 1024;
    if (swap_mib > usable_mib || home_mib > usable_mib - swap_mib) {
        std::cerr << "Swap and /home sizes exceed available disk space.\n";
        return 1;
    }
    const uint64_t root_mib = root_gib == 0
        ? usable_mib - swap_mib - home_mib : root_gib * 1024;
    if (home_mib && home_mib < 4096) {
        std::cerr << "/home must be at least 4 GiB or set it to 0.\n";
        return 1;
    }
    if (swap_mib && swap_mib < 1024) {
        std::cerr << "Swap must be at least 1 GiB or set it to 0.\n";
        return 1;
    }
    if (root_mib < 8192 || root_mib > usable_mib - swap_mib - home_mib) {
        std::cerr << "Root must be at least 8 GiB and all partitions must fit on the disk.\n";
        return 1;
    }

    int next_part = 1;
    const std::string p_efi = get_part_name(target.path, next_part++);
    const std::string p_root = get_part_name(target.path, next_part++);
    std::string p_swap, p_home;
    if (swap_mib) p_swap = get_part_name(target.path, next_part++);
    if (home_mib) p_home = get_part_name(target.path, next_part++);

    std::cout << "\n\033[1mInstallation summary\033[0m\n"
              << "Disk: " << target.path << " (" << target.size_human << ")\n"
              << "EFI: " << efi_mib << " MiB\n"
              << "Root: " << root_mib / 1024 << " GiB -> /\n";
    if (swap_mib) std::cout << "Swap: " << swap_mib / 1024 << " GiB\n";
    if (home_mib) std::cout << "Home: " << home_mib / 1024 << " GiB -> /home\n";
    const uint64_t unused_mib = usable_mib - root_mib - swap_mib - home_mib;
    std::cout << "Hostname: " << hostname << "\nUser: " << username << "\n";
    if (unused_mib) std::cout << "Unallocated: " << unused_mib << " MiB\n";
    std::cout << "\033[1;31mALL DATA ON " << target.path << " WILL BE ERASED.\033[0m\n"
              << "Type the disk path to confirm (" << target.path << "): ";
    std::string confirm;
    if (!std::getline(std::cin, confirm) || confirm != target.path) {
        std::cout << "Installation cancelled.\n";
        return 0;
    }

    const uint64_t efi_end = efi_mib + 1;
    const uint64_t root_end = efi_end + root_mib;
    std::vector<std::string> partition_commands = {
        "wipefs -a -f " + target.path,
        "parted -s " + target.path + " mklabel gpt",
        "parted -s " + target.path + " mkpart EFI fat32 1MiB " +
            std::to_string(efi_end) + "MiB",
        "parted -s " + target.path + " set 1 esp on",
        "parted -s " + target.path + " mkpart ROOT ext4 " +
            std::to_string(efi_end) + "MiB " + std::to_string(root_end) + "MiB"
    };
    uint64_t next_mib = root_end;
    if (swap_mib) {
        partition_commands.push_back("parted -s " + target.path + " mkpart SWAP linux-swap " +
            std::to_string(next_mib) + "MiB " + std::to_string(next_mib + swap_mib) + "MiB");
        next_mib += swap_mib;
    }
    if (home_mib) {
        partition_commands.push_back("parted -s " + target.path + " mkpart HOME ext4 " +
            std::to_string(next_mib) + "MiB " + std::to_string(next_mib + home_mib) + "MiB");
    }

    print_step(1, "Partitioning the target disk");
    for (const auto& command : partition_commands) {
        if (run_cmd(command) != 0) {
            std::cerr << "\033[1;31mPartitioning failed.\033[0m\n";
            return 1;
        }
    }
    if (run_cmd("udevadm settle") != 0) {
        std::cerr << "Could not refresh partition devices.\n";
        return 1;
    }
    sleep(1);

    print_step(2, "Formatting selected partitions");
    if (run_cmd("mkfs.vfat -F 32 -n EFI " + p_efi) != 0 ||
        run_cmd("mkfs.ext4 -F -L humanix-root " + p_root) != 0 ||
        (swap_mib && run_cmd("mkswap -L HUMANIX-SWAP " + p_swap) != 0) ||
        (home_mib && run_cmd("mkfs.ext4 -F -L humanix-home " + p_home) != 0)) {
        std::cerr << "\033[1;31mPartition formatting failed.\033[0m\n";
        return 1;
    }

    const std::string mnt = "/mnt/humanix_target";
    print_step(3, "Mounting target filesystems");
    fs::create_directories(mnt);
    if (run_cmd("mount " + p_root + " " + mnt) != 0) {
        std::cerr << "Could not mount root partition.\n";
        return 1;
    }
    fs::create_directories(mnt + "/boot/efi");
    if (run_cmd("mount " + p_efi + " " + mnt + "/boot/efi") != 0) {
        std::cerr << "Could not mount EFI partition.\n";
        run_cmd("umount -R " + mnt + " 2>/dev/null || true");
        return 1;
    }
    if (home_mib) {
        fs::create_directories(mnt + "/home");
        if (run_cmd("mount " + p_home + " " + mnt + "/home") != 0) {
            std::cerr << "Could not mount /home partition.\n";
            run_cmd("umount -R " + mnt + " 2>/dev/null || true");
            return 1;
        }
    }

    print_step(4, "Copying Humanix to the target");
    std::string rsync_cmd = "rsync -aAXH --numeric-ids --info=progress2 "
        "--exclude=/dev/*** --exclude=/proc/*** --exclude=/sys/*** "
        "--exclude=/tmp/*** --exclude=/run/*** --exclude=/mnt/*** "
        "--exclude=/media/*** --exclude=/home/*** --exclude=/lost+found / " + mnt + "/";
    if (run_cmd(rsync_cmd) != 0) {
        std::cerr << "\033[1;31mSystem copy failed.\033[0m\n";
        run_cmd("umount -R " + mnt + " 2>/dev/null || true");
        return 1;
    }

    std::string uuid_root = get_cmd_output("blkid -s UUID -o value " + p_root);
    std::string uuid_efi = get_cmd_output("blkid -s UUID -o value " + p_efi);
    std::string uuid_swap = swap_mib ? get_cmd_output("blkid -s UUID -o value " + p_swap) : "";
    std::string uuid_home = home_mib ? get_cmd_output("blkid -s UUID -o value " + p_home) : "";
    if (uuid_root.empty() || uuid_efi.empty() || (swap_mib && uuid_swap.empty()) ||
        (home_mib && uuid_home.empty())) {
        std::cerr << "Could not read partition UUIDs.\n";
        run_cmd("umount -R " + mnt + " 2>/dev/null || true");
        return 1;
    }

    print_step(5, "Configuring the installed system");
    std::ofstream fstab(mnt + "/etc/fstab");
    if (!fstab) {
        std::cerr << "Could not write target /etc/fstab.\n";
        run_cmd("umount -R " + mnt + " 2>/dev/null || true");
        return 1;
    }
    fstab << "UUID=" << uuid_root << " / ext4 noatime,errors=remount-ro 0 1\n"
          << "UUID=" << uuid_efi << " /boot/efi vfat umask=0077 0 2\n";
    if (swap_mib) fstab << "UUID=" << uuid_swap << " none swap sw 0 0\n";
    if (home_mib) fstab << "UUID=" << uuid_home << " /home ext4 noatime,errors=remount-ro 0 2\n";
    fstab << "tmpfs /tmp tmpfs defaults,nosuid,nodev 0 0\n";
    fstab.close();

    std::ofstream hostname_file(mnt + "/etc/hostname");
    std::ofstream hosts_file(mnt + "/etc/hosts");
    if (!hostname_file || !hosts_file) {
        std::cerr << "Could not configure hostname files.\n";
        run_cmd("umount -R " + mnt + " 2>/dev/null || true");
        return 1;
    }
    hostname_file << hostname << "\n";
    hosts_file << "127.0.0.1 localhost\n127.0.1.1 " << hostname
               << "\n\n::1 localhost ip6-localhost ip6-loopback\n";
    hostname_file.close();
    hosts_file.close();

    std::cout << "\n\033[1;36m[*] Preparing the installed system\033[0m\n";
    for (const auto& dir : {"dev", "dev/pts", "proc", "sys", "run", "tmp", "mnt", "media"}) {
        fs::create_directories(mnt + "/" + dir);
    }
    chmod((mnt + "/tmp").c_str(), 01777);
    const std::vector<std::string> bind_mounts = {
        "mount --bind /dev " + mnt + "/dev",
        "mount --bind /dev/pts " + mnt + "/dev/pts",
        "mount --bind /proc " + mnt + "/proc",
        "mount --bind /sys " + mnt + "/sys",
        "mount --bind /run " + mnt + "/run"
    };
    for (const auto& command : bind_mounts) {
        if (run_cmd(command) != 0) {
            std::cerr << "Could not prepare target chroot.\n";
            run_cmd("umount -R " + mnt + " 2>/dev/null || true");
            return 1;
        }
    }

    if (run_cmd("chroot " + mnt + " getent passwd humanix >/dev/null 2>&1") == 0 &&
        run_chroot(mnt, {"userdel", "-r", "humanix"}) != 0) {
        std::cerr << "Could not remove the default live user.\n";
        run_cmd("umount -R " + mnt + " 2>/dev/null || true");
        return 1;
    }
    const std::string target_shell = fs::exists(mnt + "/usr/local/bin/dish")
        ? "/usr/local/bin/dish" : "/bin/bash";
    if (run_chroot(mnt, {"useradd", "-m", "-s", target_shell, username}) != 0) {
        std::cerr << "Could not create the selected user.\n";
        run_cmd("umount -R " + mnt + " 2>/dev/null || true");
        return 1;
    }

    const std::vector<std::string> optional_groups = {
        "audio", "video", "netdev", "plugdev", "input", "render", "dialout", "cdrom", "dip", "users"
    };
    std::string groups = "sudo";
    for (const auto& group : optional_groups) {
        if (run_cmd("chroot " + mnt + " getent group " + group + " >/dev/null 2>&1") == 0) {
            groups += "," + group;
        }
    }
    if (run_chroot(mnt, {"usermod", "-aG", groups, username}) != 0 ||
        !set_password(mnt, username, user_password) ||
        !set_password(mnt, "root", root_password)) {
        std::cerr << "Could not configure account passwords or groups.\n";
        run_cmd("umount -R " + mnt + " 2>/dev/null || true");
        return 1;
    }

    std::ofstream sudoers(mnt + "/etc/sudoers.d/01-humanix-user");
    if (!sudoers) {
        std::cerr << "Could not configure sudo access.\n";
        run_cmd("umount -R " + mnt + " 2>/dev/null || true");
        return 1;
    }
    sudoers << username << " ALL=(ALL) NOPASSWD: ALL\n";
    sudoers.close();
    chmod((mnt + "/etc/sudoers.d/01-humanix-user").c_str(), 0440);
    fs::remove(mnt + "/etc/systemd/system/getty@tty1.service.d/autologin.conf");

    print_step(6, "Installing the UEFI bootloader");
    if (run_cmd("chroot " + mnt +
                " apt-get purge -y live-boot live-boot-initramfs-tools live-config live-config-systemd") != 0) {
        std::cerr << "Could not remove live-only packages.\n";
        run_cmd("umount -R " + mnt + " 2>/dev/null || true");
        return 1;
    }
    std::string grub_cmd = "chroot " + mnt +
        " grub-install --target=x86_64-efi --efi-directory=/boot/efi --removable --no-nvram --recheck";
    if (run_cmd(grub_cmd) != 0 ||
        run_cmd("chroot " + mnt + " update-grub") != 0) {
        std::cerr << "\033[1;31mBootloader installation failed.\033[0m\n";
        run_cmd("umount -R " + mnt + " 2>/dev/null || true");
        return 1;
    }
    std::cout << "[*] Unmounting target filesystems...\n";
    if (run_cmd("umount -R " + mnt) != 0) {
        std::cerr << "Warning: target filesystems could not be cleanly unmounted.\n";
        return 1;
    }
    std::cout << "\n\033[1;32mHumanix installed successfully on " << target.path
              << ".\033[0m\nReboot and remove the installation medium.\n";
    return 0;
}
