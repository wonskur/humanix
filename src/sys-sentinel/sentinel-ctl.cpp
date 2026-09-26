#include <iostream>
#include <string>
#include <cstring>
#include <unistd.h>
#include <sys/socket.h>
#include <sys/un.h>

#define SOCKET_PATH "/run/humanix-sentinel.sock"

int main(int argc, char* argv[]) {
    std::string cmd = (argc > 1) ? argv[1] : "STATUS";

    int fd = socket(AF_UNIX, SOCK_STREAM, 0);
    if (fd == -1) {
        std::cerr << "[-] Error opening socket" << std::endl;
        return 1;
    }

    struct sockaddr_un addr;
    std::memset(&addr, 0, sizeof(addr));
    addr.sun_family = AF_UNIX;
    std::strncpy(addr.sun_path, SOCKET_PATH, sizeof(addr.sun_path) - 1);

    if (connect(fd, (struct sockaddr*)&addr, sizeof(addr)) == -1) {
        std::cerr << "[-] Failed to connect to " << SOCKET_PATH << " (daemon down?)" << std::endl;
        close(fd);
        return 1;
    }

    cmd += "\n";
    write(fd, cmd.c_str(), cmd.size());

    char buf[1024];
    ssize_t n;
    while ((n = read(fd, buf, sizeof(buf) - 1)) > 0) {
        buf[n] = '\0';
        std::cout << buf;
    }

    close(fd);
    return 0;
}
