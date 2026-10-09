#define _DEFAULT_SOURCE
#include <iostream>
#include <vector>
#include <string>
#include <filesystem>
#include <fstream>
#include <sstream>
#include <cstdlib>
#include <csignal>
#include <cstdint>
#include <unistd.h>
#include <sys/wait.h>
#include <sys/stat.h>
#include <ftxui/component/component.hpp>
#include <ftxui/component/screen_interactive.hpp>
#include <ftxui/dom/elements.hpp>

namespace fs = std::filesystem;
using namespace ftxui;

struct BlockDevice {
    std::string name;
    std::string path;
    std::string size_human;
    std::string model;
    uint64_t size_mib = 0;
};

int run_cmd(const std::string& cmd) {
    int ret = std::system(cmd.c_str());
    if (ret == -1 || !WIFEXITED(ret)) return 1;
    return WEXITSTATUS(ret);
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

std::string get_cmd_output(const std::string& cmd) {
    char buffer[256];
    std::string result;
    FILE* pipe = popen(cmd.c_str(), "r");
    if (!pipe) return "";
    while (fgets(buffer, sizeof(buffer), pipe) != nullptr) result += buffer;
    pclose(pipe);
    while (!result.empty() && (result.back() == '\n' || result.back() == '\r')) result.pop_back();
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
            while (!base.empty() && std::isdigit(base.back())) base.pop_back();
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
        if (name.rfind("loop", 0) == 0 || name.rfind("ram", 0) == 0 || name.rfind("zram", 0) == 0 || name.rfind("sr", 0) == 0) continue;
        if (!live_media.empty() && name == live_media) continue;

        BlockDevice dev;
        dev.name = name;
        dev.path = "/dev/" + name;

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

        std::ifstream model_file(entry.path() / "device/model");
        std::string model;
        if (std::getline(model_file, model)) dev.model = model;
        else dev.model = "Generic Storage Device";

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

void perform_installation(const BlockDevice& target, const std::string& hostname, const std::string& username,
                          const std::string& root_pwd, const std::string& user_pwd, bool sudo_no_pwd) {
    uint64_t efi_mib = 512;
    uint64_t root_mib = target.size_mib - efi_mib - 2;
    std::string p_efi = get_part_name(target.path, 1);
    std::string p_root = get_part_name(target.path, 2);

    run_cmd("wipefs -a -f " + target.path);
    run_cmd("parted -s " + target.path + " mklabel gpt");
    run_cmd("parted -s " + target.path + " mkpart EFI fat32 1MiB " + std::to_string(efi_mib + 1) + "MiB");
    run_cmd("parted -s " + target.path + " set 1 esp on");
    run_cmd("parted -s " + target.path + " mkpart ROOT ext4 " + std::to_string(efi_mib + 1) + "MiB " + std::to_string(efi_mib + 1 + root_mib) + "MiB");
    run_cmd("udevadm settle");
    sleep(1);

    run_cmd("mkfs.vfat -F 32 -n EFI " + p_efi);
    run_cmd("mkfs.ext4 -F -L humanix-root " + p_root);

    const std::string mnt = "/mnt/humanix_target";
    fs::create_directories(mnt);
    run_cmd("mount " + p_root + " " + mnt);
    fs::create_directories(mnt + "/boot/efi");
    run_cmd("mount " + p_efi + " " + mnt + "/boot/efi");

    std::string rsync_cmd = "rsync -aAXH --numeric-ids "
        "--exclude=/dev/*** --exclude=/proc/*** --exclude=/sys/*** "
        "--exclude=/tmp/*** --exclude=/run/*** --exclude=/mnt/*** "
        "--exclude=/media/*** --exclude=/home/*** --exclude=/lost+found / " + mnt + "/";
    run_cmd(rsync_cmd);

    std::string uuid_root = get_cmd_output("blkid -s UUID -o value " + p_root);
    std::string uuid_efi = get_cmd_output("blkid -s UUID -o value " + p_efi);

    std::ofstream fstab(mnt + "/etc/fstab");
    fstab << "UUID=" << uuid_root << " / ext4 noatime,errors=remount-ro 0 1\n"
          << "UUID=" << uuid_efi << " /boot/efi vfat umask=0077 0 2\n"
          << "tmpfs /tmp tmpfs defaults,nosuid,nodev 0 0\n";
    fstab.close();

    std::ofstream(mnt + "/etc/hostname") << hostname << "\n";
    std::ofstream(mnt + "/etc/hosts") << "127.0.0.1 localhost\n127.0.1.1 " << hostname << "\n::1 localhost ip6-localhost ip6-loopback\n";

    for (const auto& dir : {"dev", "dev/pts", "proc", "sys", "run", "tmp", "mnt", "media"}) {
        fs::create_directories(mnt + "/" + dir);
    }
    chmod((mnt + "/tmp").c_str(), 01777);

    run_cmd("mount --bind /dev " + mnt + "/dev");
    run_cmd("mount --bind /dev/pts " + mnt + "/dev/pts");
    run_cmd("mount --bind /proc " + mnt + "/proc");
    run_cmd("mount --bind /sys " + mnt + "/sys");
    run_cmd("mount --bind /run " + mnt + "/run");

    run_chroot(mnt, {"userdel", "-r", "humanix"});
    std::string target_shell = fs::exists(mnt + "/usr/local/bin/dish") ? "/usr/local/bin/dish" : "/bin/bash";
    run_chroot(mnt, {"useradd", "-m", "-s", target_shell, username});
    run_chroot(mnt, {"usermod", "-aG", "sudo,audio,video,netdev", username});
    set_password(mnt, username, user_pwd);
    set_password(mnt, "root", root_pwd);

    std::ofstream sudoers(mnt + "/etc/sudoers.d/01-humanix-user");
    sudoers << username << (sudo_no_pwd ? " ALL=(ALL:ALL) NOPASSWD: ALL\n" : " ALL=(ALL:ALL) ALL\n");
    sudoers.close();
    chmod((mnt + "/etc/sudoers.d/01-humanix-user").c_str(), 0440);

    fs::remove(mnt + "/etc/systemd/system/getty@tty1.service.d/autologin.conf");
    fs::remove(mnt + "/usr/local/sbin/humanix-installer");

    run_cmd("chroot " + mnt + " apt-get purge -y live-boot live-boot-initramfs-tools live-config live-config-systemd live-tools");
    run_cmd("chroot " + mnt + " grub-install --target=x86_64-efi --efi-directory=/boot/efi --removable --no-nvram --recheck");
    run_cmd("chroot " + mnt + " update-grub");

    run_cmd("umount -R " + mnt + " 2>/dev/null || true");
}

int main() {
    if (geteuid() != 0) {
        std::cerr << "Run humanix-installer with sudo.\n";
        return 1;
    }

    auto disks = scan_disks();
    if (disks.empty()) {
        std::cerr << "No target disks found.\n";
        return 1;
    }

    auto screen = ScreenInteractive::Fullscreen();

    std::vector<std::string> disk_entries;
    for (const auto& d : disks) {
        disk_entries.push_back(d.path + " (" + d.size_human + ") - " + d.model);
    }

    int selected_disk_idx = 0;
    auto disk_radio = Radiobox(&disk_entries, &selected_disk_idx);

    std::string hostname = "humanix";
    std::string username = "wonskur";
    std::string user_pwd = "";
    std::string root_pwd = "";
    bool sudo_no_pwd = false;

    auto input_hostname = Input(&hostname, "hostname");
    auto input_username = Input(&username, "username");

    InputOption pwd_option;
    pwd_option.password = true;
    auto input_user_pwd = Input(&user_pwd, "password", pwd_option);
    auto input_root_pwd = Input(&root_pwd, "password", pwd_option);

    auto check_sudo = Checkbox("Passwordless sudo", &sudo_no_pwd);

    bool start_install = false;
    auto btn_install = Button("Install Humanix", [&] {
        if (!user_pwd.empty() && !root_pwd.empty()) {
            start_install = true;
            screen.ExitLoopClosure()();
        }
    });

    auto btn_cancel = Button("Exit", screen.ExitLoopClosure());

    auto container = Container::Vertical({
        disk_radio,
        input_hostname,
        input_username,
        input_user_pwd,
        input_root_pwd,
        check_sudo,
        Container::Horizontal({btn_install, btn_cancel})
    });

    auto renderer = Renderer(container, [&] {
        return vbox({
            text(" Humanix OS Installer (TUI) ") | bold | hcenter | color(Color::Cyan),
            separator(),
            vbox({
                text("1. Select Destination Disk (WILL BE WIPED):") | bold | color(Color::Yellow),
                disk_radio->Render() | frame | size(HEIGHT, LESS_THAN, 6),
            }) | border,
            vbox({
                text("2. System & User Configuration:") | bold | color(Color::Green),
                hbox(text("Hostname:   "), input_hostname->Render()),
                hbox(text("Username:   "), input_username->Render()),
                hbox(text("User Pass:  "), input_user_pwd->Render()),
                hbox(text("Root Pass:  "), input_root_pwd->Render()),
                check_sudo->Render(),
            }) | border,
            hbox({
                btn_install->Render() | color(Color::Green) | bold,
                text("  "),
                btn_cancel->Render() | color(Color::Red),
            }) | hcenter
        }) | borderDouble;
    });

    screen.Loop(renderer);

    if (start_install) {
        std::cout << "\033[2J\033[H";
        std::cout << "Starting installation on " << disks[selected_disk_idx].path << "...\n";
        perform_installation(disks[selected_disk_idx], hostname, username, root_pwd, user_pwd, sudo_no_pwd);
        std::cout << "\nInstallation completed! Reboot and remove media.\n";
    }

    return 0;
}
