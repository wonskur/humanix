#define _DEFAULT_SOURCE

#include <ftxui/component/component.hpp>
#include <ftxui/component/screen_interactive.hpp>
#include <ftxui/dom/elements.hpp>

#include <algorithm>
#include <array>
#include <cctype>
#include <cerrno>
#include <cstdio>
#include <cstring>
#include <filesystem>
#include <fstream>
#include <functional>
#include <iostream>
#include <sstream>
#include <string>
#include <sys/stat.h>
#include <sys/types.h>
#include <sys/wait.h>
#include <unistd.h>
#include <vector>

namespace fs = std::filesystem;
using namespace ftxui;

namespace {
constexpr const char* LOG_PATH = "/var/log/humanix-installer.log";
constexpr const char* TARGET_MOUNT = "/mnt/humanix-target";

struct Result {
    int code = 1;
    std::string output;
    bool ok = false;
};

std::ofstream log_stream;

void log(const std::string& message) {
    if (log_stream) {
        log_stream << message << '\n';
        log_stream.flush();
    }
}

std::string trim(std::string s) {
    while (!s.empty() && std::isspace(static_cast<unsigned char>(s.back()))) s.pop_back();
    size_t i = 0;
    while (i < s.size() && std::isspace(static_cast<unsigned char>(s[i]))) ++i;
    return s.substr(i);
}

std::string shell_quote_for_log(const std::string& s) {
    std::string out = "'";
    for (char c : s) {
        if (c == '\'') out += "'\\''";
        else out += c;
    }
    return out + "'";
}

// Executes a program directly, without a shell. Captured output is also written to the log.
Result run(const std::vector<std::string>& args, bool capture = true,
           const std::string& stdin_data = "") {
    Result r;
    if (args.empty()) {
        r.output = "Empty command";
        return r;
    }

    std::ostringstream command;
    command << "$";
    for (const auto& arg : args) command << ' ' << shell_quote_for_log(arg);
    log(command.str());

    int out_pipe[2] = {-1, -1};
    int in_pipe[2] = {-1, -1};
    if (capture && pipe(out_pipe) != 0) {
        r.output = std::string("pipe: ") + std::strerror(errno);
        return r;
    }
    if (!stdin_data.empty() && pipe(in_pipe) != 0) {
        if (capture) { close(out_pipe[0]); close(out_pipe[1]); }
        r.output = std::string("pipe: ") + std::strerror(errno);
        return r;
    }

    pid_t pid = fork();
    if (pid < 0) {
        if (capture) { close(out_pipe[0]); close(out_pipe[1]); }
        if (!stdin_data.empty()) { close(in_pipe[0]); close(in_pipe[1]); }
        r.output = std::string("fork: ") + std::strerror(errno);
        return r;
    }

    if (pid == 0) {
        if (capture) {
            close(out_pipe[0]);
            dup2(out_pipe[1], STDOUT_FILENO);
            dup2(out_pipe[1], STDERR_FILENO);
            close(out_pipe[1]);
        }
        if (!stdin_data.empty()) {
            close(in_pipe[1]);
            dup2(in_pipe[0], STDIN_FILENO);
            close(in_pipe[0]);
        }
        std::vector<char*> argv;
        argv.reserve(args.size() + 1);
        for (const auto& arg : args) argv.push_back(const_cast<char*>(arg.c_str()));
        argv.push_back(nullptr);
        execvp(argv[0], argv.data());
        _exit(errno == ENOENT ? 127 : 126);
    }

    if (capture) close(out_pipe[1]);
    if (!stdin_data.empty()) {
        close(in_pipe[0]);
        size_t offset = 0;
        while (offset < stdin_data.size()) {
            ssize_t n = write(in_pipe[1], stdin_data.data() + offset, stdin_data.size() - offset);
            if (n < 0 && errno == EINTR) continue;
            if (n <= 0) break;
            offset += static_cast<size_t>(n);
        }
        close(in_pipe[1]);
    }

    if (capture) {
        char buffer[4096];
        ssize_t n;
        while ((n = read(out_pipe[0], buffer, sizeof(buffer))) > 0)
            r.output.append(buffer, static_cast<size_t>(n));
        close(out_pipe[0]);
    }

    int status = 0;
    while (waitpid(pid, &status, 0) < 0) {
        if (errno == EINTR) continue;
        r.output += "\nwaitpid failed";
        log(r.output);
        return r;
    }
    r.code = WIFEXITED(status) ? WEXITSTATUS(status) : 128;
    r.ok = r.code == 0;
    if (!r.output.empty()) log(trim(r.output));
    log("exit code: " + std::to_string(r.code));
    return r;
}

bool command_exists(const std::string& command) {
    // Only called with constant program names.
    return run({"sh", "-c", "command -v " + command + " >/dev/null 2>&1"}, false).ok;
}

bool write_file(const fs::path& path, const std::string& contents, std::string& error) {
    std::ofstream file(path);
    if (!file) {
        error = "Cannot write " + path.string();
        return false;
    }
    file << contents;
    if (!file) {
        error = "Failed while writing " + path.string();
        return false;
    }
    return true;
}

std::string partition_path(const std::string& disk, int number) {
    if (disk.find("nvme") != std::string::npos ||
        disk.find("mmcblk") != std::string::npos ||
        disk.find("loop") != std::string::npos) {
        return disk + "p" + std::to_string(number);
    }
    return disk + std::to_string(number);
}

std::string live_disk_name() {
    std::ifstream mounts("/proc/mounts");
    std::string line;
    while (std::getline(mounts, line)) {
        std::istringstream input(line);
        std::string source, mountpoint;
        input >> source >> mountpoint;
        if (mountpoint != "/run/live/medium" &&
            mountpoint != "/lib/live/mount/medium" &&
            mountpoint != "/lib/live/mount/rootfs/filesystem.squashfs") continue;

        std::string name = fs::path(source).filename().string();
        // Remove partition suffix, handling NVMe and MMC devices.
        if (name.rfind("nvme", 0) == 0 || name.rfind("mmcblk", 0) == 0) {
            auto p = name.rfind('p');
            if (p != std::string::npos && p + 1 < name.size() &&
                std::all_of(name.begin() + static_cast<long>(p + 1), name.end(),
                            [](unsigned char c) { return std::isdigit(c); }))
                name.resize(p);
        } else {
            while (!name.empty() && std::isdigit(static_cast<unsigned char>(name.back())))
                name.pop_back();
        }
        return name;
    }
    return "";
}

struct Disk {
    std::string name;
    std::string path;
    std::string model;
    std::string size;
    uint64_t mib = 0;
    bool mounted = false;
};

std::vector<Disk> scan_disks() {
    std::vector<Disk> result;
    const std::string live = live_disk_name();

    for (const auto& entry : fs::directory_iterator("/sys/block")) {
        const std::string name = entry.path().filename().string();
        if (name.rfind("loop", 0) == 0 || name.rfind("ram", 0) == 0 ||
            name.rfind("zram", 0) == 0 || name.rfind("sr", 0) == 0 ||
            name.rfind("dm-", 0) == 0) continue;
        if (!live.empty() && name == live) continue;

        Disk disk;
        disk.name = name;
        disk.path = "/dev/" + name;

        std::ifstream size_file(entry.path() / "size");
        uint64_t sectors = 0;
        if (!(size_file >> sectors)) continue;
        disk.mib = sectors / 2048;
        char size_buffer[48];
        std::snprintf(size_buffer, sizeof(size_buffer), "%.1f GiB",
                      static_cast<double>(sectors) / 2097152.0);
        disk.size = size_buffer;

        std::ifstream model_file(entry.path() / "device/model");
        if (!std::getline(model_file, disk.model) || trim(disk.model).empty())
            disk.model = "Storage device";
        disk.model = trim(disk.model);

        // Refuse disks with mounted descendants. lsblk's output is parsed, not executed by a shell.
        auto lsblk = run({"lsblk", "-nrpo", "MOUNTPOINT", disk.path});
        std::istringstream mounts(lsblk.output);
        std::string mountpoint;
        while (std::getline(mounts, mountpoint)) {
            if (!trim(mountpoint).empty()) disk.mounted = true;
        }
        result.push_back(disk);
    }
    return result;
}

bool valid_hostname(const std::string& s) {
    if (s.empty() || s.size() > 63 || s.front() == '-' || s.back() == '-') return false;
    return std::all_of(s.begin(), s.end(), [](unsigned char c) {
        return std::isalnum(c) || c == '-';
    });
}

bool valid_username(const std::string& s) {
    if (s.empty() || s.size() > 32) {
        return false;
    }

    const unsigned char first = static_cast<unsigned char>(s.front());

    if ((!std::islower(first)) && (s.front() != '_')) {
        return false;
    }

    return std::all_of(s.begin(), s.end(), [](unsigned char c) {
        return std::islower(c) || std::isdigit(c) || c == '_' || c == '-';
    });
}

bool mounted_at(const std::string& target) {
    std::ifstream mounts("/proc/mounts");
    std::string source, mountpoint;
    while (mounts >> source >> mountpoint) {
        std::string rest;
        std::getline(mounts, rest);
        if (mountpoint == target || mountpoint.rfind(target + "/", 0) == 0) return true;
    }
    return false;
}

bool run_checked(const std::vector<std::string>& args, const std::string& action,
                 std::string& error) {
    auto r = run(args);
    if (!r.ok) {
        error = action + " failed (exit " + std::to_string(r.code) + "). See " + LOG_PATH;
        return false;
    }
    return true;
}

bool chroot_checked(const std::string& root, const std::vector<std::string>& args,
                    const std::string& action, std::string& error) {
    std::vector<std::string> command = {"chroot", root};
    command.insert(command.end(), args.begin(), args.end());
    return run_checked(command, action, error);
}

// The input is piped to chpasswd, never put in argv or a shell command.
bool set_password_with_stdin(const std::string& root, const std::string& username,
                             const std::string& password, std::string& error) {
    auto r = run({"chroot", root, "chpasswd"}, true, username + ":" + password + "\n");
    if (!r.ok) {
        error = "Could not set password for " + username + ". See " + LOG_PATH;
        return false;
    }
    return true;
}

void unmount_target(const std::string& root) {
    run({"umount", "-R", root});
}

struct Config {
    int disk_index = 0;
    int filesystem = 0; // 0 ext4, 1 btrfs
    int swap = 1;       // 0 none, 1 2 GiB, 2 4 GiB, 3 8 GiB, 4 16 GiB
    int timezone = 0;
    int locale = 0;
    int keymap = 0;
    int desktop = 0;    // none, XFCE, KDE, GNOME
    int bootloader = 0; // GRUB UEFI only
    bool install_ssh = false;
    bool passwordless_sudo = false;
    std::string hostname = "humanix";
    std::string username = "user";
    std::string user_password;
    std::string root_password;
    bool confirmed_wipe = false;
};

std::vector<std::string> FILESYSTEMS = {"ext4", "btrfs"};
std::vector<std::string> SWAP_OPTIONS = {"No swap", "2 GiB", "4 GiB", "8 GiB", "16 GiB"};
std::vector<std::string> TIMEZONES = {"UTC", "Europe/Vienna", "Europe/Moscow", "Europe/London",
                                            "America/New_York", "Asia/Tokyo"};
std::vector<std::string> LOCALES = {"en_US.UTF-8", "ru_RU.UTF-8", "de_DE.UTF-8", "fr_FR.UTF-8"};
std::vector<std::string> KEYMAPS = {"us", "ru", "de", "fr"};
std::vector<std::string> DESKTOPS = {"None (console)", "XFCE", "KDE Plasma", "GNOME"};
std::vector<std::string> BOOTLOADERS = {"GRUB (UEFI)"};

struct InstallResult {
    bool ok = false;
    std::string error;
};

InstallResult install_system(const Disk& disk, const Config& cfg,
                             const std::function<void(const std::string&, int)>& progress) {
    InstallResult result;
    std::string error;
    const std::string root = TARGET_MOUNT;
    const std::string p_efi = partition_path(disk.path, 1);
    const std::string p_root = partition_path(disk.path, 2);
    const std::string p_swap = partition_path(disk.path, 3);
    const uint64_t efi_mib = 1024;
    const std::array<uint64_t, 5> swap_mib_values = {0, 2048, 4096, 8192, 16384};
    const uint64_t swap_mib = swap_mib_values[static_cast<size_t>(cfg.swap)];

    struct stat st {};
    if (stat(disk.path.c_str(), &st) != 0 || !S_ISBLK(st.st_mode)) {
        result.error = "Selected disk is not a block device anymore: " + disk.path;
        return result;
    }
    if (disk.mounted) {
        result.error = "The selected disk has mounted partitions. Unmount them and restart the installer.";
        return result;
    }
    if (disk.mib < efi_mib + swap_mib + 2048) {
        result.error = "Disk is too small for the selected EFI, root and swap partitions.";
        return result;
    }
    if (!fs::exists("/sys/firmware/efi")) {
        result.error = "This installer was booted in BIOS/Legacy mode. Boot the live USB in UEFI mode.";
        return result;
    }

    const uint64_t root_end = disk.mib - swap_mib - 1;
    progress("Wiping signatures and creating GPT partitions", 5);
    if (!run_checked({"wipefs", "-a", "-f", disk.path}, "Clearing disk signatures", error) ||
        !run_checked({"parted", "-s", disk.path, "mklabel", "gpt"}, "Creating GPT partition table", error) ||
        !run_checked({"parted", "-s", disk.path, "mkpart", "EFI", "fat32", "1MiB",
                      std::to_string(efi_mib + 1) + "MiB"}, "Creating EFI partition", error) ||
        !run_checked({"parted", "-s", disk.path, "set", "1", "esp", "on"}, "Marking EFI partition", error) ||
        !run_checked({"parted", "-s", disk.path, "mkpart", "ROOT", FILESYSTEMS[cfg.filesystem],
                      std::to_string(efi_mib + 1) + "MiB", std::to_string(root_end) + "MiB"},
                     "Creating root partition", error)) {
        result.error = error;
        return result;
    }
    if (swap_mib > 0 &&
        !run_checked({"parted", "-s", disk.path, "mkpart", "SWAP", "linux-swap",
                      std::to_string(root_end) + "MiB", std::to_string(disk.mib - 1) + "MiB"},
                     "Creating swap partition", error)) {
        result.error = error;
        return result;
    }

    progress("Waiting for partition devices", 12);
    if (!run_checked({"partprobe", disk.path}, "Refreshing partition table", error) ||
        !run_checked({"udevadm", "settle"}, "Waiting for udev", error)) {
        result.error = error;
        return result;
    }
    usleep(300000);

    progress("Formatting EFI and root filesystems", 18);
    if (!run_checked({"mkfs.vfat", "-F", "32", "-n", "HUMANIX_EFI", p_efi},
                     "Formatting EFI partition", error)) {
        result.error = error; return result;
    }
    if (cfg.filesystem == 0) {
        if (!run_checked({"mkfs.ext4", "-F", "-L", "humanix-root", p_root},
                         "Formatting ext4 root partition", error)) {
            result.error = error; return result;
        }
    } else {
        if (!run_checked({"mkfs.btrfs", "-f", "-L", "humanix-root", p_root},
                         "Formatting Btrfs root partition", error)) {
            result.error = error; return result;
        }
    }
    if (swap_mib > 0 &&
        !run_checked({"mkswap", "-L", "humanix-swap", p_swap}, "Formatting swap partition", error)) {
        result.error = error; return result;
    }

    progress("Mounting target filesystems", 24);
    fs::create_directories(root);
    if (!run_checked({"mount", p_root, root}, "Mounting root filesystem", error)) {
        result.error = error; return result;
    }
    fs::create_directories(root + "/boot/efi");
    if (!run_checked({"mount", p_efi, root + "/boot/efi"}, "Mounting EFI filesystem", error)) {
        unmount_target(root); result.error = error; return result;
    }

    progress("Cloning the live system to the target disk", 32);
    const std::vector<std::string> rsync = {
        "rsync", "-aAXH", "--numeric-ids", "--delete",
        "--exclude=/dev/***", "--exclude=/proc/***", "--exclude=/sys/***",
        "--exclude=/tmp/***", "--exclude=/run/***", "--exclude=/mnt/***",
        "--exclude=/media/***", "--exclude=/lost+found",
        "--exclude=/lib/live/mount/***", "--exclude=/run/live/***",
        "/", root + "/"
    };
    if (!run_checked(rsync, "Cloning live filesystem with rsync", error)) {
        unmount_target(root); result.error = error; return result;
    }

    progress("Writing fstab and system identity", 62);
    auto uuid_root = run({"blkid", "-s", "UUID", "-o", "value", p_root});
    auto uuid_efi = run({"blkid", "-s", "UUID", "-o", "value", p_efi});
    if (!uuid_root.ok || trim(uuid_root.output).empty() ||
        !uuid_efi.ok || trim(uuid_efi.output).empty()) {
        unmount_target(root);
        result.error = "Could not read partition UUIDs; refusing to write an incomplete fstab.";
        return result;
    }
    std::ostringstream fstab;
    const std::string root_opts = cfg.filesystem == 0 ? "noatime,errors=remount-ro" : "noatime,compress=zstd";
    fstab << "UUID=" << trim(uuid_root.output) << " / " << FILESYSTEMS[cfg.filesystem]
          << ' ' << root_opts << " 0 1\n";
    fstab << "UUID=" << trim(uuid_efi.output) << " /boot/efi vfat umask=0077 0 2\n";
    if (swap_mib > 0) {
        auto uuid_swap = run({"blkid", "-s", "UUID", "-o", "value", p_swap});
        if (!uuid_swap.ok || trim(uuid_swap.output).empty()) {
            unmount_target(root);
            result.error = "Could not read swap UUID.";
            return result;
        }
        fstab << "UUID=" << trim(uuid_swap.output) << " none swap sw 0 0\n";
    }
    fstab << "tmpfs /tmp tmpfs defaults,nosuid,nodev 0 0\n";
    if (!write_file(root + "/etc/fstab", fstab.str(), error) ||
        !write_file(root + "/etc/hostname", cfg.hostname + "\n", error) ||
        !write_file(root + "/etc/hosts",
                    "127.0.0.1 localhost\n127.0.1.1 " + cfg.hostname +
                    "\n::1 localhost ip6-localhost ip6-loopback\n", error)) {
        unmount_target(root); result.error = error; return result;
    }

    progress("Preparing chroot and configuring user accounts", 68);
    for (const auto& dir : {"dev", "dev/pts", "proc", "sys", "run", "tmp", "mnt", "media"}) {
        fs::create_directories(root + "/" + dir);
    }
    chmod((root + "/tmp").c_str(), 01777);
    const std::vector<std::pair<std::string, std::string>> bind_mounts = {
        {"/dev", root + "/dev"}, {"/dev/pts", root + "/dev/pts"},
        {"/proc", root + "/proc"}, {"/sys", root + "/sys"}, {"/run", root + "/run"}
    };
    for (const auto& m : bind_mounts) {
        if (!run_checked({"mount", "--bind", m.first, m.second}, "Bind-mounting " + m.first, error)) {
            unmount_target(root); result.error = error; return result;
        }
    }

    // Remove the live-session default account if it exists; a missing account is not fatal.
    run({"chroot", root, "userdel", "-r", "humanix"});
    if (!chroot_checked(root, {"useradd", "-m", "-s", "/bin/bash", cfg.username},
                        "Creating user account", error)) {
        unmount_target(root); result.error = error; return result;
    }
    run({"chroot", root, "usermod", "-aG", "sudo", cfg.username});
    if (!set_password_with_stdin(root, cfg.username, cfg.user_password, error)) {
        unmount_target(root); result.error = error; return result;
    }
    if (!cfg.root_password.empty()) {
        if (!set_password_with_stdin(root, "root", cfg.root_password, error)) {
            unmount_target(root); result.error = error; return result;
        }
    } else {
        run({"chroot", root, "passwd", "-l", "root"});
    }

    fs::create_directories(root + "/etc/sudoers.d");
    std::string sudoers = cfg.username +
        (cfg.passwordless_sudo ? " ALL=(ALL:ALL) NOPASSWD: ALL\n" : " ALL=(ALL:ALL) ALL\n");
    if (!write_file(root + "/etc/sudoers.d/90-humanix-user", sudoers, error)) {
        unmount_target(root); result.error = error; return result;
    }
    chmod((root + "/etc/sudoers.d/90-humanix-user").c_str(), 0440);

    const std::string timezone_file = root + "/usr/share/zoneinfo/" + TIMEZONES[cfg.timezone];
    if (!fs::exists(timezone_file)) {
        unmount_target(root);
        result.error = "Timezone data not found in the cloned system: " + TIMEZONES[cfg.timezone];
        return result;
    }
    fs::remove(root + "/etc/localtime");
    fs::create_symlink("/usr/share/zoneinfo/" + TIMEZONES[cfg.timezone], root + "/etc/localtime");

    // Enable the selected locale if its entry exists in locale.gen, then generate it.
    {
        const std::string locale_file = root + "/etc/locale.gen";
        std::ifstream in(locale_file);
        std::ostringstream out;
        std::string line;
        bool found = false;
        while (std::getline(in, line)) {
            std::string t = trim(line);
            if (t == "# " + LOCALES[cfg.locale] || t == LOCALES[cfg.locale]) {
                out << LOCALES[cfg.locale] << '\n';
                found = true;
            } else {
                out << line << '\n';
            }
        }
        if (in.good() || in.eof()) {
            if (!found) out << LOCALES[cfg.locale] << '\n';
            write_file(locale_file, out.str(), error);
        }
        write_file(root + "/etc/default/locale", "LANG=" + LOCALES[cfg.locale] + "\n", error);
    }
    write_file(root + "/etc/default/keyboard", "XKBLAYOUT=\"" + KEYMAPS[cfg.keymap] + "\"\n", error);

    progress("Installing optional packages", 77);
    std::vector<std::string> packages;
    if (cfg.install_ssh) packages.push_back("openssh-server");
    if (cfg.desktop == 1) packages.insert(packages.end(), {"xorg", "xfce4", "xfce4-goodies", "lightdm"});
    if (cfg.desktop == 2) packages.insert(packages.end(), {"xorg", "kde-plasma-desktop", "sddm"});
    if (cfg.desktop == 3) packages.insert(packages.end(), {"xorg", "gnome-core", "gdm3"});
    if (!packages.empty()) {
        std::vector<std::string> apt = {"chroot", root, "apt-get", "update"};
        if (!run_checked(apt, "Updating package lists in target system", error)) {
            unmount_target(root); result.error = error; return result;
        }
        apt = {"chroot", root, "apt-get", "install", "-y"};
        apt.insert(apt.end(), packages.begin(), packages.end());
        if (!run_checked(apt, "Installing selected packages", error)) {
            unmount_target(root); result.error = error; return result;
        }
    }
    if (cfg.install_ssh) {
        run({"chroot", root, "systemctl", "enable", "ssh"});
    }
    if (cfg.desktop == 1) run({"chroot", root, "systemctl", "enable", "lightdm"});
    if (cfg.desktop == 2) run({"chroot", root, "systemctl", "enable", "sddm"});
    if (cfg.desktop == 3) run({"chroot", root, "systemctl", "enable", "gdm3"});

    progress("Installing and configuring GRUB for UEFI", 88);
    if (!fs::exists(root + "/usr/sbin/grub-install")) {
        if (!chroot_checked(root, {"apt-get", "update"}, "Updating package lists for GRUB", error) ||
            !chroot_checked(root, {"apt-get", "install", "-y", "grub-efi-amd64"}, "Installing GRUB EFI", error)) {
            unmount_target(root); result.error = error; return result;
        }
    }
    if (!chroot_checked(root, {"grub-install", "--target=x86_64-efi", "--efi-directory=/boot/efi",
                               "--bootloader-id=Humanix", "--recheck"},
                        "Installing GRUB EFI bootloader", error)) {
        // Firmware implementations without writable NVRAM can still boot the fallback path.
        log("GRUB NVRAM install failed; trying removable-media fallback.");
        if (!chroot_checked(root, {"grub-install", "--target=x86_64-efi", "--efi-directory=/boot/efi",
                                   "--bootloader-id=Humanix", "--removable", "--no-nvram", "--recheck"},
                            "Installing fallback GRUB EFI bootloader", error)) {
            unmount_target(root); result.error = error; return result;
        }
    }
    if (!chroot_checked(root, {"update-grub"}, "Generating GRUB configuration", error)) {
        unmount_target(root); result.error = error; return result;
    }

    progress("Finalizing installation and unmounting filesystems", 96);
    // Remove known live-session conveniences if they were cloned.
    fs::remove(root + "/etc/systemd/system/getty@tty1.service.d/autologin.conf");
    unmount_target(root);
    run({"sync"});
    progress("Installation completed", 100);
    result.ok = true;
    return result;
}
} // namespace

int main() {
    if (geteuid() != 0) {
        std::cerr << "Run as root: sudo ./humanix-installer\n";
        return 1;
    }

    log_stream.open(LOG_PATH, std::ios::app);
    if (!log_stream) {
        std::cerr << "Cannot open log file " << LOG_PATH << '\n';
        return 1;
    }
    log("\n=== Humanix Installer started ===");

    if (!fs::exists("/sys/firmware/efi")) {
        std::cerr << "Warning: the live system is booted in Legacy/BIOS mode. This installer currently targets UEFI.\n";
    }

    auto disks = scan_disks();
    if (disks.empty()) {
        std::cerr << "No eligible disks found. Check lsblk and the live USB.\n";
        return 1;
    }

    Config cfg;
    std::vector<std::string> disk_options;
    for (const auto& d : disks) {
        std::string entry = d.path + "  •  " + d.size + "  •  " + d.model;
        if (d.mounted) entry += "  [HAS MOUNTED PARTITIONS — NOT SELECTABLE]";
        disk_options.push_back(entry);
    }

    std::vector<std::string> pages = {
        "Disk & partitions", "Bootloader", "Filesystem & swap", "Localization",
        "User account", "Desktop & services", "Review & install"
    };
    int page = 0;
    int selected_disk = 0;
    int filesystem = cfg.filesystem;
    int swap = cfg.swap;
    int timezone = cfg.timezone;
    int locale = cfg.locale;
    int keymap = cfg.keymap;
    int desktop = cfg.desktop;
    int bootloader = cfg.bootloader;

    auto page_menu = Menu(&pages, &page);
    auto disk_menu = Radiobox(&disk_options, &selected_disk);
    auto filesystem_menu = Radiobox(&FILESYSTEMS, &filesystem);
    auto swap_menu = Radiobox(&SWAP_OPTIONS, &swap);
    auto timezone_menu = Radiobox(&TIMEZONES, &timezone);
    auto locale_menu = Radiobox(&LOCALES, &locale);
    auto keymap_menu = Radiobox(&KEYMAPS, &keymap);
    auto desktop_menu = Radiobox(&DESKTOPS, &desktop);
    auto bootloader_menu = Radiobox(&BOOTLOADERS, &bootloader);

    auto input_hostname = Input(&cfg.hostname, "humanix");
    auto input_username = Input(&cfg.username, "username");
    InputOption password_option;
    password_option.password = true;
    auto input_user_password = Input(&cfg.user_password, "required password", password_option);
    auto input_root_password = Input(&cfg.root_password, "optional; empty locks root", password_option);
    auto ssh_cb = Checkbox("Install and enable OpenSSH server", &cfg.install_ssh);
    auto nopass_cb = Checkbox("Allow passwordless sudo for this user", &cfg.passwordless_sudo);
    auto wipe_cb = Checkbox("I understand: the selected disk will be erased completely", &cfg.confirmed_wipe);

    std::string ui_error;
    bool request_install = false;
    bool quit = false;
    auto screen = ftxui::ScreenInteractive::Fullscreen();
    auto back_button = Button("Back", [&] { if (page > 0) --page; });
    auto next_button = Button("Next", [&] { if (page < static_cast<int>(pages.size()) - 1) ++page; });
    auto install_button = Button("Install", [&] {
        cfg.disk_index = selected_disk;
        cfg.filesystem = filesystem;
        cfg.swap = swap;
        cfg.timezone = timezone;
        cfg.locale = locale;
        cfg.keymap = keymap;
        cfg.desktop = desktop;
        cfg.bootloader = bootloader;

        if (selected_disk < 0 || selected_disk >= static_cast<int>(disks.size())) {
            ui_error = "Select a target disk.";
            return;
        }
        if (disks[selected_disk].mounted) {
            ui_error = "That disk has mounted partitions. Select another disk.";
            return;
        }
        if (!valid_hostname(cfg.hostname)) {
            ui_error = "Hostname must be 1–63 letters, digits or hyphens; it cannot start/end with a hyphen.";
            return;
        }
        if (!valid_username(cfg.username)) {
            ui_error = "Username must start with a lowercase letter or underscore and contain only lowercase letters, digits, _ or -.";
            return;
        }
        if (cfg.user_password.size() < 8) {
            ui_error = "Choose a user password of at least 8 characters.";
            return;
        }
        if (!cfg.confirmed_wipe) {
            ui_error = "Confirm that the selected disk will be erased.";
            return;
        }
        if (!fs::exists("/sys/firmware/efi")) {
            ui_error = "Boot the live USB in UEFI mode before installing.";
            return;
        }
        if (cfg.disk_index < 0 || cfg.disk_index >= static_cast<int>(disks.size()) ||
            disks[cfg.disk_index].mib < 1024 + 2048) {
            ui_error = "Selected disk is too small.";
            return;
        }
        request_install = true;
        quit = true;
        ui_error.clear();
    });
        auto quit_button = Button("Quit", [&] {
        quit = true;
        screen.Exit();
    }); // Quit without installing
    

    auto page_container = Container::Tab({
        Container::Vertical({disk_menu}),
        Container::Vertical({bootloader_menu}),
        Container::Vertical({filesystem_menu, swap_menu}),
        Container::Vertical({timezone_menu, locale_menu, keymap_menu}),
        Container::Vertical({input_hostname, input_username, input_user_password, input_root_password, nopass_cb}),
        Container::Vertical({desktop_menu, ssh_cb}),
        Container::Vertical({wipe_cb})
    }, &page);

    auto footer = Container::Horizontal({back_button, next_button, install_button, quit_button});
    auto root_container = Container::Vertical({Container::Horizontal({page_menu, page_container}), footer});

    auto selected_disk_summary = [&]() -> std::string {
        if (selected_disk < 0 || selected_disk >= static_cast<int>(disks.size())) return "No disk selected";
        return disks[selected_disk].path + " — " + disks[selected_disk].size + " — " + disks[selected_disk].model;
    };

    auto page_content = [&]() -> Element {
        switch (page) {
            case 0:
                return vbox({
                    text("Select installation disk") | bold | color(Color::Cyan),
                    text("The chosen disk and all its partitions will be erased.") | color(Color::Yellow),
                    separator(),
                    disk_menu->Render() | vscroll_indicator | frame | flex,
                    separator(),
                    text("Selected: " + selected_disk_summary())
                });
            case 1:
                return vbox({
                    text("Bootloader") | bold | color(Color::Cyan),
                    text("Current version supports UEFI systems and installs GRUB.") ,
                    separator(), bootloader_menu->Render() | frame
                });
            case 2:
                return vbox({
                    text("Root filesystem") | bold | color(Color::Cyan),
                    filesystem_menu->Render(),
                    separator(),
                    text("Swap partition") | bold | color(Color::Cyan),
                    swap_menu->Render(),
                    separator(),
                    text("The installer creates a 1 GiB EFI System Partition and a root partition.")
                });
            case 3:
                return vbox({
                    text("Localization") | bold | color(Color::Cyan),
                    text("Timezone"), timezone_menu->Render(),
                    separator(), text("Locale"), locale_menu->Render(),
                    separator(), text("Keyboard layout"), keymap_menu->Render()
                });
            case 4:
                return vbox({
                    text("User account") | bold | color(Color::Cyan),
                    hbox(text("Hostname: "), input_hostname->Render()),
                    hbox(text("Username: "), input_username->Render()),
                    hbox(text("Password: "), input_user_password->Render()),
                    hbox(text("Root password: "), input_root_password->Render()),
                    text("Leave root password empty to lock direct root login.") | dim,
                    separator(), nopass_cb->Render()
                });
            case 5:
                return vbox({
                    text("Desktop environment") | bold | color(Color::Cyan),
                    desktop_menu->Render(),
                    separator(),
                    ssh_cb->Render(),
                    text("A desktop selection installs its Xorg, desktop and matching display manager packages.") | dim
                });
            default:
                return vbox({
                    text("Review configuration") | bold | color(Color::Cyan),
                    separator(),
                    text("Target disk: " + selected_disk_summary()),
                    text("Partition table: GPT"),
                    text("Bootloader: " + BOOTLOADERS[bootloader]),
                    text("Root filesystem: " + FILESYSTEMS[filesystem]),
                    text("Swap: " + SWAP_OPTIONS[swap]),
                    text("Timezone: " + TIMEZONES[timezone]),
                    text("Locale: " + LOCALES[locale]),
                    text("Keyboard: " + KEYMAPS[keymap]),
                    text("Hostname: " + cfg.hostname),
                    text("Username: " + cfg.username),
                    text("Desktop: " + DESKTOPS[desktop]),
                    text(std::string("OpenSSH: ") + (cfg.install_ssh ? "Yes" : "No")),
                    text(std::string("Passwordless sudo: ") + (cfg.passwordless_sudo ? "Yes" : "No")),
                    separator(),
                    wipe_cb->Render() | color(Color::Red) | bold,
                    text("Press Install only after confirming the target disk.") | color(Color::Yellow)
                });
        }
    };

    auto renderer = Renderer(root_container, [&] {
        Elements nav;
        for (size_t i = 0; i < pages.size(); ++i) {
            nav.push_back(text((static_cast<int>(i) == page ? "> " : "  ") + pages[i]) |
                          (static_cast<int>(i) == page ? bold | color(Color::Cyan) : dim));
        }
        return vbox({
            hbox({
                vbox({
                    text("HUMANIX") | bold | color(Color::Cyan),
                    text("Installer") | dim,
                    separator(),
                    vbox(nav) | flex,
                    separator(),
                    text("↑↓ Navigate") | dim,
                    text("Enter Select") | dim,
                    text("Tab Focus") | dim
                }) | size(WIDTH, EQUAL, 25) | border,
                page_content() | flex | border
            }) | flex,
            text(ui_error.empty() ? "" : "Error: " + ui_error) | color(Color::Red) | bold,
            hbox({
                back_button->Render(),
                next_button->Render(),
                install_button->Render(),
                quit_button->Render(),
                filler(),
                text("Log: " + std::string(LOG_PATH)) | dim
            }) | border
        }) | borderDouble;
    });

    screen.Loop(renderer);
    if (!request_install) {
        std::cout << (quit ? "Installer closed.\n" : "Installation cancelled.\n");
        return 0;
    }

    cfg.disk_index = selected_disk;
    cfg.filesystem = filesystem;
    cfg.swap = swap;
    cfg.timezone = timezone;
    cfg.locale = locale;
    cfg.keymap = keymap;
    cfg.desktop = desktop;
    cfg.bootloader = bootloader;

    std::cout << "\033[2J\033[H";
    std::cout << "Humanix Installer\nTarget: " << disks[cfg.disk_index].path
              << "\nLog: " << LOG_PATH << "\n\n";
    std::cout << "WARNING: all data on " << disks[cfg.disk_index].path << " will be destroyed.\n";

    auto progress = [](const std::string& message, int percent) {
        std::cout << "[" << percent << "%] " << message << '\n' << std::flush;
        log("[" + std::to_string(percent) + "%] " + message);
    };

    auto result = install_system(disks[cfg.disk_index], cfg, progress);
    if (!result.ok) {
        std::cerr << "\nINSTALLATION FAILED: " << result.error << '\n'
                  << "Review " << LOG_PATH << " before retrying.\n";
        log("FATAL: " + result.error);
        return 1;
    }

    std::cout << "\nHumanix was installed successfully.\n"
              << "Remove the live USB and reboot when ready.\n";
    std::string answer;
    std::cout << "Reboot now? [y/N]: " << std::flush;
    std::getline(std::cin, answer);
    std::transform(answer.begin(), answer.end(), answer.begin(),
                   [](unsigned char c) { return static_cast<char>(std::tolower(c)); });
    if (answer == "y" || answer == "yes") run({"systemctl", "reboot"});
    return 0;
}