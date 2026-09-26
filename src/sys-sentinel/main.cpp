#include <iostream>
#include <fstream>
#include <string>
#include <chrono>
#include <thread>
#include <csignal>
#include <atomic>
#include <unistd.h>

std::atomic<bool> keep_running(true);

void signal_handler(int) {
    keep_running = false;
}

int main() {
    std::signal(SIGTERM, signal_handler);
    std::signal(SIGINT, signal_handler);

    std::cout << "[HUMANIX-SENTINEL] Core monitor initialized (PID: " << getpid() << ")" << std::endl;

    while (keep_running) {
        std::ifstream uptime_file("/proc/uptime");
        if (uptime_file.is_open()) {
            double uptime_sec;
            uptime_file >> uptime_sec;
            std::cout << "[HUMANIX-SENTINEL] Node heartbeat | Uptime: " << uptime_sec << "s" << std::endl;
        } else {
            std::cerr << "[HUMANIX-SENTINEL] Error reading /proc/uptime" << std::endl;
        }

        for (int i = 0; i < 30 && keep_running; ++i) {
            std::this_thread::sleep_for(std::chrono::seconds(1));
        }
    }

    std::cout << "[HUMANIX-SENTINEL] Terminated cleanly." << std::endl;
    return 0;
}
