#include <iostream>
#include <fstream>
#include <sstream>
#include <string>
#include <vector>
#include <cstring>
#include <csignal>
#include <unistd.h>
#include <sys/socket.h>
#include <sys/un.h>
#include <sys/epoll.h>
#include <sys/sysinfo.h>
#include <sys/stat.h>
#include <fcntl.h>

#define SOCKET_PATH "/run/humanix-sentinel.sock"
#define MAX_EVENTS 16
#define BUFFER_SIZE 512

static int server_fd = -1;
static volatile sig_atomic_t g_running = 1;

void sig_handler(int) {
    g_running = 0;
}

int set_nonblocking(int fd) {
    int flags = fcntl(fd, F_GETFL, 0);
    return (flags == -1) ? -1 : fcntl(fd, F_SETFL, flags | O_NONBLOCK);
}

std::string get_status_report() {
    struct sysinfo si;
    sysinfo(&si);

    long total_ram = (si.totalram * si.mem_unit) / (1024 * 1024);
    long free_ram = (si.freeram * si.mem_unit) / (1024 * 1024);
    long used_ram = total_ram - free_ram;

    std::ostringstream oss;
    oss << "{\n"
        << "  \"daemon\": \"sys-sentinel\",\n"
        << "  \"uptime_seconds\": " << si.uptime << ",\n"
        << "  \"loads\": [" << (si.loads[0] / 65536.0) << ", "
                          << (si.loads[1] / 65536.0) << ", "
                          << (si.loads[2] / 65536.0) << "],\n"
        << "  \"ram\": {\"total_mb\": " << total_ram << ", \"used_mb\": " << used_ram << "},\n"
        << "  \"procs\": " << si.procs << "\n"
        << "}\n";
    return oss.str();
}

int main() {
    signal(SIGTERM, sig_handler);
    signal(SIGINT, sig_handler);

    unlink(SOCKET_PATH);

    server_fd = socket(AF_UNIX, SOCK_STREAM, 0);
    if (server_fd == -1) {
        perror("socket");
        return 1;
    }

    struct sockaddr_un addr;
    std::memset(&addr, 0, sizeof(addr));
    addr.sun_family = AF_UNIX;
    std::strncpy(addr.sun_path, SOCKET_PATH, sizeof(addr.sun_path) - 1);

    if (bind(server_fd, (struct sockaddr*)&addr, sizeof(addr)) == -1) {
        perror("bind");
        close(server_fd);
        return 1;
    }

    chmod(SOCKET_PATH, 0666);

    if (listen(server_fd, SOMAXCONN) == -1) {
        perror("listen");
        close(server_fd);
        unlink(SOCKET_PATH);
        return 1;
    }

    set_nonblocking(server_fd);

    int epoll_fd = epoll_create1(0);
    if (epoll_fd == -1) {
        perror("epoll_create1");
        close(server_fd);
        unlink(SOCKET_PATH);
        return 1;
    }

    struct epoll_event ev, events[MAX_EVENTS];
    ev.events = EPOLLIN;
    ev.data.fd = server_fd;
    epoll_ctl(epoll_fd, EPOLL_CTL_ADD, server_fd, &ev);

    std::cout << "[SYS-SENTINEL] IPC Socket listening on " << SOCKET_PATH << std::endl;

    char buf[BUFFER_SIZE];

    while (g_running) {
        int nfds = epoll_wait(epoll_fd, events, MAX_EVENTS, 500);
        if (nfds == -1) {
            if (errno == EINTR) continue;
            break;
        }

        for (int i = 0; i < nfds; ++i) {
            if (events[i].data.fd == server_fd) {
                int client_fd = accept(server_fd, nullptr, nullptr);
                if (client_fd != -1) {
                    set_nonblocking(client_fd);
                    struct epoll_event client_ev;
                    client_ev.events = EPOLLIN | EPOLLET;
                    client_ev.data.fd = client_fd;
                    epoll_ctl(epoll_fd, EPOLL_CTL_ADD, client_fd, &client_ev);
                }
            } else {
                int client_fd = events[i].data.fd;
                std::memset(buf, 0, sizeof(buf));
                ssize_t bytes_read = read(client_fd, buf, sizeof(buf) - 1);

                if (bytes_read <= 0) {
                    epoll_ctl(epoll_fd, EPOLL_CTL_DEL, client_fd, nullptr);
                    close(client_fd);
                } else {
                    std::string cmd(buf);
                    while (!cmd.empty() && (cmd.back() == '\n' || cmd.back() == '\r')) {
                        cmd.pop_back();
                    }

                    std::string resp;
                    if (cmd == "PING") {
                        resp = "PONG\n";
                    } else if (cmd == "STATUS") {
                        resp = get_status_report();
                    } else if (cmd == "UPTIME") {
                        struct sysinfo si;
                        sysinfo(&si);
                        resp = std::to_string(si.uptime) + "\n";
                    } else {
                        resp = "ERROR: UNKNOWN_COMMAND\n";
                    }

                    write(client_fd, resp.c_str(), resp.size());
                    epoll_ctl(epoll_fd, EPOLL_CTL_DEL, client_fd, nullptr);
                    close(client_fd);
                }
            }
        }
    }

    close(server_fd);
    close(epoll_fd);
    unlink(SOCKET_PATH);
    std::cout << "[SYS-SENTINEL] Daemon stopped cleanly." << std::endl;
    return 0;
}
