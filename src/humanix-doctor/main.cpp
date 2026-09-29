#include <cstdlib>
#include <iostream>
#include <string>

static bool run_quiet(const std::string& cmd) {
    std::string full = cmd + " >/dev/null 2>&1";
    return std::system(full.c_str()) == 0;
}
static void check_networkmanager() {
    std::cout << "[NetworkManager]\n";
    if (run_quiet("systemctl is-active --quiet NetworkManager")) {
        std::cout << "OK: support service\n";
    } else {
        std::cout << "FAIL: NetworkManager not active\n";
        std::cout << "CHECK: systemctl status NetworkManager\n";
        std::cout << "CHECK: systemctl enable --now NetworkManager\n";
    }
}
int main() {
    std::cout << "humanix-doctor: quick  check system\n";
    check_networkmanager();
    return 0;
}