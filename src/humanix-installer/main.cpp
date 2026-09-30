#include <iostream>
#include <vector>
#include <string>
#include <filesystem>
#include <fstream>
#include <sstream>
#include <cstdlib>
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
    bool is_removable;
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

int main() {
    if (geteuid() != 0) {
        std::cerr << "\033[1;31m[ERROR] humanix-installer must be executed with root privileges.\033[0m" << std::endl;
        return 1;
    }

    std::cout << "\033[1;36m"
              << "==============================================\n"
              << "              HUMANIX INSTALLER               \n"
              << "          Native UEFI installation            \n"
              << "==============================================\033[0m\n"
              << "Install Humanix onto a physical drive.\n\n";

    const std::vector<std::string> required = {
        "wipefs", "parted", "udevadm", "mkfs.vfat", "mkfs.ext4",
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
        std::cerr << "\033[1;31m[-] No valid target storage drives found!\033[0m" << std::endl;
        return 1;
    }

    std::cout << "\033[1mAvailable drives\033[0m\n";
    for (size_t i = 0; i < disks.size(); ++i) {
        std::cout << "  \033[1;36m" << (i + 1) << ")\033[0m "
                  << disks[i].path << "  " << disks[i].size_human
                  << "  \033[90m" << disks[i].model << "\033[0m\n";
    }

    std::cout << "\nSelect drive number to install Humanix [1-" << disks.size() << "]: ";
    int choice = 0;
    if (!(std::cin >> choice) || choice < 1 || choice > static_cast<int>(disks.size())) {
        std::cerr << "[-] Invalid selection. Aborting." << std::endl;
        return 1;
    }

    BlockDevice target = disks[choice - 1];

    std::cout << "\n\033[1;31mWARNING: ALL DATA ON " << target.path
              << " WILL BE ERASED.\033[0m\n"
              << "Review the selected drive above before continuing.\n"
              << "To confirm, type the drive path (" << target.path << "): ";
    std::string confirm;
    std::cin >> confirm;
    if (confirm != target.path) {
        std::cout << "Installation cancelled by user." << std::endl;
        return 0;
    }

    std::string p_efi = get_part_name(target.path, 1);
    std::string p_root = get_part_name(target.path, 2);

    print_step(1, "Partitioning the target drive");
    if (run_cmd("wipefs -a -f " + target.path) != 0 ||
        run_cmd("parted -s " + target.path + " mklabel gpt") != 0 ||
        run_cmd("parted -s " + target.path + " mkpart ESP fat32 1MiB 513MiB") != 0 ||
        run_cmd("parted -s " + target.path + " set 1 esp on") != 0 ||
        run_cmd("parted -s " + target.path + " mkpart ROOT ext4 513MiB 100%") != 0 ||
        run_cmd("udevadm settle") != 0) {
        std::cerr << "\033[1;31mPartitioning failed.\033[0m\n";
        return 1;
    }
    sleep(1);

    print_step(2, "Formatting EFI and root partitions");
    if (run_cmd("mkfs.vfat -F 32 -n EFI " + p_efi) != 0) {
        std::cerr << "[-] Failed to format EFI partition." << std::endl;
        return 1;
    }
    if (run_cmd("mkfs.ext4 -F -L humanix-root " + p_root) != 0) {
        std::cerr << "[-] Failed to format Root partition." << std::endl;
        return 1;
    }

    print_step(3, "Mounting target filesystems");
    const std::string mnt = "/mnt/humanix_target";
    fs::create_directories(mnt);
    if (run_cmd("mount " + p_root + " " + mnt) != 0) {
        std::cerr << "\033[1;31mCould not mount the root partition.\033[0m\n";
        return 1;
    }
    fs::create_directories(mnt + "/boot/efi");
    if (run_cmd("mount " + p_efi + " " + mnt + "/boot/efi") != 0) {
        std::cerr << "\033[1;31mCould not mount the EFI partition.\033[0m\n";
        run_cmd("umount -R " + mnt + " 2>/dev/null || true");
        return 1;
    }

    print_step(4, "Copying Humanix to the target");
    std::string rsync_cmd = "rsync -aAXH --numeric-ids --info=progress2 "
                            "--exclude=/dev/*** --exclude=/proc/*** --exclude=/sys/*** "
                            "--exclude=/tmp/*** --exclude=/run/*** --exclude=/mnt/*** "
                            "--exclude=/media/*** --exclude=/lost+found "
                            "/ " + mnt + "/";
    if (run_cmd(rsync_cmd) != 0) {
        std::cerr << "\n\033[1;31mCopy failed; installation did not finish.\033[0m\n";
        run_cmd("umount -R " + mnt + " 2>/dev/null || true");
        return 1;
    }

    print_step(5, "Writing filesystem configuration");
    std::string uuid_root = get_cmd_output("blkid -s UUID -o value " + p_root);
    std::string uuid_efi = get_cmd_output("blkid -s UUID -o value " + p_efi);

    std::ofstream fstab(mnt + "/etc/fstab");
    if (!fstab) {
        std::cerr << "\033[1;31mCould not write target /etc/fstab.\033[0m\n";
        run_cmd("umount -R " + mnt + " 2>/dev/null || true");
        return 1;
    }
    fstab << "# /etc/fstab: generated by humanix-installer\n"
          << "UUID=" << uuid_root << " / ext4 noatime,errors=remount-ro 0 1\n"
          << "UUID=" << uuid_efi << " /boot/efi vfat umask=0077 0 2\n"
          << "tmpfs /tmp tmpfs defaults,nosuid,nodev 0 0\n";
    fstab.close();

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
            std::cerr << "\033[1;31mCould not prepare the target chroot.\033[0m\n";
            run_cmd("umount -R " + mnt + " 2>/dev/null || true");
            return 1;
        }
    }

    print_step(6, "Installing the UEFI bootloader");
    if (run_cmd("chroot " + mnt + " apt-get purge -y live-boot live-boot-initramfs-tools") != 0) {
        std::cerr << "\033[1;31mCould not remove live-only packages.\033[0m\n";
        run_cmd("umount -R " + mnt + " 2>/dev/null || true");
        return 1;
    }
    std::string grub_cmd = "chroot " + mnt + " grub-install --target=x86_64-efi --efi-directory=/boot/efi --bootloader-id=Humanix --recheck";
    if (run_cmd(grub_cmd) != 0 ||
        run_cmd("chroot " + mnt + " update-grub") != 0) {
        std::cerr << "\033[1;31mBootloader installation failed.\033[0m\n";
        run_cmd("umount -R " + mnt + " 2>/dev/null || true");
        return 1;
    }
    std::cout << "[*] Unmounting targets..." << std::endl;
    run_cmd("umount -R " + mnt + " 2>/dev/null || true");

    std::cout << "\n\033[1;32m[✓] HUMANIX HAS BEEN SUCCESSFULLY INSTALLED TO " << target.path << "!\033[0m\n"
              << "You can now reboot and remove your installation medium.\n";
    return 0;
}