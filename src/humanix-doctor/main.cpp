/*
 * Humanix OS - humanix-doctor (Diagnostic Tool)
 * Copyright (C) 2026 wonskur
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <https://www.gnu.org/licenses/>.
 */

#include <cstdlib>
#include <iostream>
#include <string>
#include <sys/statvfs.h>
#include <sys/sysinfo.h>

static bool run_quiet(const std::string& cmd) {
    std::string full = cmd + " >/dev/null 2>&1";
    return std::system(full.c_str()) == 0;
}

static bool check_networkmanager() {
    std::cout << "[1/8 NetworkManager]\n";
    if (run_quiet("systemctl is-active --quiet NetworkManager")) {
        std::cout << "  OK: NetworkManager is active\n";
        return true;
    }
    std::cout << "  FAIL: NetworkManager is inactive\n";
    std::cout << "  FIX:  sudo systemctl enable --now NetworkManager\n";
    return false;
}

static bool check_default_route() {
    std::cout << "[2/8 Default Route]\n";
    if (run_quiet("ip route show default | grep -q '^default '")) {
        std::cout << "  OK: Default gateway exists\n";
        return true;
    }
    std::cout << "  FAIL: No default route\n";
    std::cout << "  FIX:  Check network cable or run 'nmtui'\n";
    return false;
}

static bool check_dns() {
    std::cout << "[3/8 DNS Resolution]\n";
    if (run_quiet("getent ahosts deb.debian.org")) {
        std::cout << "  OK: deb.debian.org resolves\n";
        return true;
    }
    std::cout << "  FAIL: DNS cannot resolve domains\n";
    std::cout << "  FIX:  Check /etc/resolv.conf or run 'resolvectl status'\n";
    return false;
}

static bool check_ping() {
    std::cout << "[4/8 Internet Connectivity]\n";
    if (run_quiet("ping -c 1 -W 2 1.1.1.1")) {
        std::cout << "  OK: External ping to 1.1.1.1 succeeded\n";
        return true;
    }
    std::cout << "  FAIL: No ICMP response from 1.1.1.1\n";
    std::cout << "  FIX:  Check firewall or WAN connection\n";
    return false;
}

static bool check_root_disk_space() {
    std::cout << "[5/8 Root Disk Space (/)]\n";
    struct statvfs stat;
    if (statvfs("/", &stat) != 0) {
        std::cout << "  FAIL: Could not stat root filesystem\n";
        return false;
    }

    unsigned long long free_bytes = stat.f_bavail * stat.f_frsize;
    unsigned long long free_mb = free_bytes / (1024 * 1024);

    if (free_mb < 500) {
        std::cout << "  FAIL: Root partition is almost full (" << free_mb << " MB left)\n";
        std::cout << "  FIX:  sudo apt clean && sudo rm -rf /var/log/*.gz\n";
        return false;
    }
    std::cout << "  OK: " << free_mb << " MB available on /\n";
    return true;
}

static bool check_ram() {
    std::cout << "[6/8 RAM Status]\n";
    struct sysinfo info;
    if (sysinfo(&info) != 0) {
        std::cout << "  FAIL: Could not query sysinfo\n";
        return false;
    }

    unsigned long long free_mb = (info.freeram * info.mem_unit) / (1024 * 1024);
    if (free_mb < 100) {
        std::cout << "  WARN: Critically low RAM (" << free_mb << " MB free)\n";
        return false;
    }
    std::cout << "  OK: " << free_mb << " MB free RAM\n";
    return true;
}

static bool check_failed_services() {
    std::cout << "[7/8 Systemd Degraded Services]\n";
    if (run_quiet("systemctl --failed --quiet | grep -q '0 loaded units listed' || ! systemctl is-system-running --quiet")) {
        if (!run_quiet("test $(systemctl --failed --no-legend | wc -l) -eq 0")) {
            std::cout << "  FAIL: One or more systemd services failed\n";
            std::cout << "  FIX:  systemctl --failed\n";
            return false;
        }
    }
    std::cout << "  OK: No failed systemd units\n";
    return true;
}

static bool check_apt_locks() {
    std::cout << "[8/8 Package Manager Lock]\n";
    if (run_quiet("fuser /var/lib/dpkg/lock-frontend") || run_quiet("fuser /var/lib/apt/lists/lock")) {
        std::cout << "  WARN: APT/dpkg database is locked by another process\n";
        std::cout << "  CHECK: ps aux | grep -E 'apt|dpkg'\n";
        return false;
    }
    std::cout << "  OK: dpkg database is unlocked\n";
    return true;
}

int main() {
    std::cout << "\033[1;36mHumanix Doctor: System Diagnostics\033[0m\n\n";

    bool ok = true;
    ok = check_networkmanager() && ok;
    ok = check_default_route() && ok;
    ok = check_dns() && ok;
    ok = check_ping() && ok;
    ok = check_root_disk_space() && ok;
    ok = check_ram() && ok;
    ok = check_failed_services() && ok;
    ok = check_apt_locks() && ok;
    if (ok) {
        std::cout << "\033[1;32m[PASS] All system checks passed successfully.\033[0m\n";
        return 0;
    } else {
        std::cout << "\033[1;31m[WARN] Diagnostic found issues above.\033[0m\n";
        return 1;
    }
}