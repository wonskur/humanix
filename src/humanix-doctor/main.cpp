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

static bool run_quiet(const std::string& cmd) {
    std::string full = cmd + " >/dev/null 2>&1";
    return std::system(full.c_str()) == 0;
}

static bool check_networkmanager() {
    std::cout << "[NetworkManager]\n";
    if (run_quiet("systemctl is-active --quiet NetworkManager")) {
        std::cout << "OK: service is active\n";
        return true;
    }
    std::cout << "FAIL: service is not active\n";
    std::cout << "CHECK: systemctl status NetworkManager\n";
    std::cout << "FIX: systemctl enable --now NetworkManager\n";
    return false;
}

static bool check_default_route() {
    std::cout << "[Default route]\n";
    if (run_quiet("ip route show default | grep -q '^default '")) {
        std::cout << "OK: route is configured\n";
        return true;
    }
    std::cout << "FAIL: no default route\n";
    std::cout << "CHECK: ip route; nmcli device status\n";
    return false;
}

static bool check_dns() {
    std::cout << "[DNS]\n";
    if (run_quiet("getent ahosts deb.debian.org")) {
        std::cout << "OK: deb.debian.org resolves\n";
        return true;
    }
    std::cout << "FAIL: DNS lookup did not resolve deb.debian.org\n";
    std::cout << "CHECK: resolvectl status\n";
    return false;
}

int main() {
    std::cout << "humanix-doctor: quick system check\n";
    bool ok = check_networkmanager();
    ok = check_default_route() && ok;
    ok = check_dns() && ok;
    std::cout << (ok ? "Result: all checks passed\n" : "Result: issues found\n");
    return ok ? 0 : 1;
}
