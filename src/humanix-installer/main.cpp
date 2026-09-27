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
    std::cout << "\033[90m[EXEC] " << cmd << "\033[0m" << std::endl;
    int ret = std::system(cmd.c_str());
    return WEXITSTATUS(ret);
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
              << "----------------------------------------------------\n"
              << "         HUMANIX NATIVE BARE-METAL INSTALLER        \n"
              << "____________________________________________________\033[0m\n\n";

    auto disks = scan_disks();
    if (disks.empty()) {
        std::cerr << "\033[1;31m[-] No valid target storage drives found!\033[0m" << std::endl;
        return 1;
    }

    std::cout << "Available target drives:\n";
    for (size_t i = 0; i < disks.size(); ++i) {
        std::cout << "  [" << (i + 1) << "] " << disks[i].path << " (" << disks[i].size_human << ") - " << disks[i].model << "\n";
    }

    std::cout << "\nSelect drive number to install Humanix [1-" << disks.size() << "]: ";
    int choice = 0;
    if (!(std::cin >> choice) || choice < 1 || choice > static_cast<int>(disks.size())) {
        std::cerr << "[-] Invalid selection. Aborting." << std::endl;
        return 1;
    }

    BlockDevice target = disks[choice - 1];

    std::cout << "\n\033[1;31m[WARNING] ALL DATA ON " << target.path << " WILL BE PERMANENTLY DESTROYED!\033[0m\n";
    std::cout << "Type 'YES' to confirm partitioning and installation: ";
    std::string confirm;
    std::cin >> confirm;
    if (confirm != "YES") {
        std::cout << "Installation cancelled by user." << std::endl;
        return 0;
    }

    std::string p_efi = get_part_name(target.path, 1);
    std::string p_root = get_part_name(target.path, 2);

    std::cout << "\n[1/6] Partitioning disk with GPT layout..." << std::endl;
    run_cmd("wipefs -a -f " + target.path);
    run_cmd("parted -s " + target.path + " mklabel gpt");
    run_cmd("parted -s " + target.path + " mkpart ESP fat32 1MiB 513MiB");
    run_cmd("parted -s " + target.path + " set 1 esp on");
    run_cmd("parted -s " + target.path + " mkpart ROOT ext4 513MiB 100%");
    run_cmd("udevadm settle");
    sleep(1);

    std::cout << "[2/6] Formatting partitions..." << std::endl;
    if (run_cmd("mkfs.vfat -F 32 -n EFI " + p_efi) != 0) {
        std::cerr << "[-] Failed to format EFI partition." << std::endl;
        return 1;
    }
    if (run_cmd("mkfs.ext4 -F -L humanix-root " + p_root) != 0) {
        std::cerr << "[-] Failed to format Root partition." << std::endl;
        return 1;
    }

    std::cout << "[3/6] Mounting target filesystems..." << std::endl;
    const std::string mnt = "/mnt/humanix_target";
    fs::create_directories(mnt);
    run_cmd("mount " + p_root + " " + mnt);
    fs::create_directories(mnt + "/boot/efi");
    run_cmd("mount " + p_efi + " " + mnt + "/boot/efi");

    std::cout << "[4/6] Synchronizing OS tree (this may take a few minutes)..." << std::endl;
    std::string rsync_cmd = "rsync -aAXH --info=progress2 "
                            "--exclude={'/dev/*','/proc/*','/sys/*','/tmp/*','/run/*','/mnt/*','/media/*','/lost+found'} "
                            "/ " + mnt + "/";
    if (run_cmd(rsync_cmd) != 0) {
        std::cerr << "[-] Rsync synchronization failed." << std::endl;
        return 1;
    }

    std::cout << "[5/6] Generating /etc/fstab..." << std::endl;
    std::string uuid_root = get_cmd_output("blkid -s UUID -o value " + p_root);
    std::string uuid_efi = get_cmd_output("blkid -s UUID -o value " + p_efi);

    std::ofstream fstab(mnt + "/etc/fstab");
    fstab << "# /etc/fstab: generated by humanix-installer\n"
          << "UUID=" << uuid_root << " / ext4 noatime,errors=remount-ro 0 1\n"
          << "UUID=" << uuid_efi << " /boot/efi vfat umask=0077 0 2\n"
          << "tmpfs /tmp tmpfs defaults,nosuid,nodev 0 0\n";
    fstab.close();

    std::cout << "[6/6] Installing UEFI bootloader and kernel initramfs..." << std::endl;
    run_cmd("mount --bind /dev " + mnt + "/dev");
    run_cmd("mount --bind /dev/pts " + mnt + "/dev/pts");
    run_cmd("mount --bind /proc " + mnt + "/proc");
    run_cmd("mount --bind /sys " + mnt + "/sys");
    run_cmd("mount --bind /run " + mnt + "/run");
    run_cmd("chroot " + mnt + " apt-get purge -y live-boot live-boot-initramfs-tools");
    std::string grub_cmd = "chroot " + mnt + " grub-install --target=x86_64-efi --efi-directory=/boot/efi --bootloader-id=Humanix --recheck";
    run_cmd(grub_cmd);
    run_cmd("chroot " + mnt + " update-grub");
    std::cout << "[*] Unmounting targets..." << std::endl;
    run_cmd("umount -R " + mnt + " 2>/dev/null || true");

    std::cout << "\n\033[1;32m[✓] HUMANIX HAS BEEN SUCCESSFULLY INSTALLED TO " << target.path << "!\033[0m\n"
              << "You can now reboot and remove your installation medium.\n";
    return 0;
}