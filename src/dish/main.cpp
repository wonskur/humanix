#include <algorithm>
#include <cerrno>
#include <cctype>
#include <csignal>
#include <cstdio>
#include <cstdlib>
#include <cstring>

#include <fcntl.h>
#include <fstream>
#include <iostream>
#include <limits.h>
#include <locale.h>
#include <map>
#include <memory>
#include <sstream>
#include <string>
#include <sys/ioctl.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <sys/wait.h>
#include <termios.h>
#include <unistd.h>
#include <vector>

using namespace std;

vector<string> history;
int history_idx = -1;
string saved_input = "";
struct termios orig_termios;
bool termios_saved = false;
int last_exit_status = 0;
pid_t last_background_pid = 0;
size_t last_rendered_rows = 0;

map<string, string> aliases;

static const vector<string> builtin_names = {
    "cd", "export", "unset", "exit", "history", "jobs", "dishhelp",
    "alias", "unalias", "source", "."
};

void restore_terminal() {
    if (termios_saved) {
        tcsetattr(0, TCSAFLUSH, &orig_termios);
    }
}

void on_fatal_signal(int sig) {
    restore_terminal();
    signal(sig, SIG_DFL);
    raise(sig);
}

size_t get_utf8_char_len(unsigned char c) {
    if ((c & 0x80) == 0x00) return 1;
    if ((c & 0xE0) == 0xC0) return 2;
    if ((c & 0xF0) == 0xE0) return 3;
    if ((c & 0xF8) == 0xF0) return 4;
    return 1;
}

size_t utf8_char_count(const string& s) {
    size_t count = 0;
    for (size_t i = 0; i < s.size(); ) {
        size_t len = get_utf8_char_len(static_cast<unsigned char>(s[i]));
        i += len;
        count++;
    }
    return count;
}

string get_current_dir() {
    char cwd[PATH_MAX];
    if (getcwd(cwd, sizeof(cwd)) != nullptr) {
        return string(cwd);
    }
    return string(".");
}

string get_hostname_str() {
    const char* h = getenv("HOSTNAME");
    if (h && *h) return string(h);
    char buf[256];
    if (gethostname(buf, sizeof(buf)) == 0) return string(buf);
    return "humanix";
}

string get_prompt_text() {
    const char* user = getenv("USER");
    return string(user ? user : "humanix") + "@" + get_hostname_str() + ":" + get_current_dir() + "> ";
}

void print_prompt() {
    const char* user = getenv("USER");
    cout << "\033[1;32m" << (user ? user : "humanix") << "@" << get_hostname_str() << "\033[0m:"
         << "\033[1;34m" << get_current_dir() << "\033[0m> ";
    cout.flush();
    last_rendered_rows = 0;
}

int get_term_columns() {
    struct winsize ws;
    if (ioctl(STDOUT_FILENO, TIOCGWINSZ, &ws) == 0 && ws.ws_col > 0) {
        return ws.ws_col;
    }
    return 80;
}

bool is_builtin_command(const string& cmd);

bool is_executable_file(const string& path) {
    struct stat sb;
    return (stat(path.c_str(), &sb) == 0 && S_ISREG(sb.st_mode) && (sb.st_mode & 0111));
}

bool is_command_valid(const string& cmd) {
    if (cmd.empty()) return false;
    if (is_builtin_command(cmd) || aliases.count(cmd)) return true;
    if (cmd.find('/') != string::npos) {
        return is_executable_file(cmd);
    }
    const char* path_env = getenv("PATH");
    if (!path_env) return false;

    stringstream ss(path_env);
    string dir;
    while (getline(ss, dir, ':')) {
        string full = dir.empty() ? cmd : dir + "/" + cmd;
        if (is_executable_file(full)) return true;
    }
    return false;
}

string find_suggestion(const string& input) {
    if (input.empty()) return "";
    for (int i = static_cast<int>(history.size()) - 1; i >= 0; --i) {
        const string& h = history[i];
        if (h.size() > input.size() && h.compare(0, input.size(), input) == 0) {
            size_t first_space = h.find(' ');
            string first_word = (first_space == string::npos) ? h : h.substr(0, first_space);
            if (is_command_valid(first_word)) {
                return h.substr(input.size());
            }
        }
    }
    return "";
}

void redraw_line(const string& input, size_t cursor_byte_pos) {
    int cols = get_term_columns();
    string prompt = get_prompt_text();
    size_t prompt_len = utf8_char_count(prompt);

    if (last_rendered_rows > 0) {
        cout << "\033[" << last_rendered_rows << "A";
    }
    cout << "\r\033[K";

    const char* user = getenv("USER");
    cout << "\033[1;32m" << (user ? user : "humanix") << "@" << get_hostname_str() << "\033[0m:"
         << "\033[1;34m" << get_current_dir() << "\033[0m> ";

    string suggestion = find_suggestion(input);

    cout << "\033[0m" << input;
    if (!suggestion.empty()) {
        cout << "\033[90m" << suggestion << "\033[0m";
    }
    cout << "\033[J";

    size_t input_chars = utf8_char_count(input);
    size_t total_chars = prompt_len + input_chars + utf8_char_count(suggestion);
    last_rendered_rows = total_chars / cols;

    size_t cursor_char_pos = utf8_char_count(input.substr(0, cursor_byte_pos));
    size_t target_col = prompt_len + cursor_char_pos;
    size_t target_row = target_col / cols;
    size_t target_x = (target_col % cols) + 1;

    size_t diff_up = last_rendered_rows - target_row;
    if (diff_up > 0) {
        cout << "\033[" << diff_up << "A";
    }
    cout << "\r";
    if (target_x > 1) {
        cout << "\033[" << (target_x - 1) << "C";
    }
    cout.flush();
}

void install_shell_signal_handlers() {
    signal(SIGINT, SIG_IGN);
    signal(SIGQUIT, SIG_IGN);
    signal(SIGTSTP, SIG_IGN);
    signal(SIGTTOU, SIG_IGN);
    signal(SIGTTIN, SIG_IGN);
    signal(SIGTERM, on_fatal_signal);
    signal(SIGHUP, on_fatal_signal);
    atexit(restore_terminal);
}

string expand_tilde(const string& path) {
    if (path.empty() || path[0] != '~') return path;
    const char* home = getenv("HOME");
    string home_str = home ? home : ".";
    if (path.size() == 1) return home_str;
    if (path[1] == '/' || path[1] == '\\') {
        return home_str + path.substr(1);
    }
    return path;
}

string expand_environment_variables(const string& token) {
    string result = expand_tilde(token);

    for (size_t i = 0; i < result.size(); ++i) {
        if (result[i] != '$') continue;
        if (i + 1 >= result.size()) continue;

        if (result[i+1] == '$') {
            string pid = to_string(getpid());
            result.replace(i, 2, pid);
            i += pid.size() - 1;
            continue;
        }
        if (result[i+1] == '?') {
            string s = to_string(last_exit_status);
            result.replace(i, 2, s);
            i += s.size() - 1;
            continue;
        }
        if (result[i+1] == '!') {
            string s = to_string(last_background_pid);
            result.replace(i, 2, s);
            i += s.size() - 1;
            continue;
        }

        size_t start = i + 1;
        if (result[start] == '{') {
            size_t end = result.find('}', start);
            if (end != string::npos) {
                string name = result.substr(start + 1, end - start - 1);
                if (!name.empty()) {
                    const char* value = getenv(name.c_str());
                    if (value) {
                        result.replace(i, end - i + 1, value);
                        i += string(value).size() - 1;
                    }
                }
                continue;
            }
        }

        if (isalnum((unsigned char)result[start]) || result[start] == '_') {
            size_t j = start + 1;
            while (j < result.size() && (isalnum((unsigned char)result[j]) || result[j] == '_')) {
                ++j;
            }
            string name = result.substr(start, j - start);
            const char* value = getenv(name.c_str());
            if (value) {
                result.replace(i, j - i, value);
                i += string(value).size() - 1;
            }
        }
    }
    return result;
}

vector<string> split_shell_tokens(const string& input) {
    vector<string> tokens;
    string token;
    token.reserve(32);
    bool in_single_quote = false;
    bool in_double_quote = false;

    for (size_t i = 0; i < input.size(); ++i) {
        unsigned char ch = static_cast<unsigned char>(input[i]);

        if (ch == '\\' && !in_single_quote) {
            if (i + 1 < input.size()) token += input[++i];
            else token += '\\';
            continue;
        }

        if (in_single_quote) {
            if (ch == '\'') in_single_quote = false;
            else token += input[i];
            continue;
        }

        if (in_double_quote) {
            if (ch == '"') {
                in_double_quote = false;
            } else {
                if (ch == '\\' && i + 1 < input.size() &&
                    (input[i + 1] == '"' || input[i + 1] == '$' || input[i + 1] == '\\')) {
                    token += input[++i];
                } else {
                    token += input[i];
                }
            }
            continue;
        }

        if (ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r') {
            if (!token.empty()) {
                tokens.push_back(token);
                token.clear();
            }
            continue;
        }

        if (ch == '\'') { in_single_quote = true; continue; }
        if (ch == '"') { in_double_quote = true; continue; }

        if (ch == '|' || ch == '<' || ch == '>') {
            if (!token.empty()) {
                tokens.push_back(token);
                token.clear();
            }
            if (ch == '>' && i + 1 < input.size() && input[i + 1] == '>') {
                tokens.emplace_back(">>");
                ++i;
            } else {
                tokens.emplace_back(1, input[i]);
            }
            continue;
        }

        token += input[i];
    }
    if (!token.empty()) tokens.push_back(token);
    return tokens;
}

struct Command {
    vector<string> argv;
    string input_file;
    string output_file;
    bool append_output = false;
};

void apply_environment_assignments(vector<string>& argv) {
    vector<string> filtered;
    filtered.reserve(argv.size());
    bool at_start = true;
    for (const string& token : argv) {
        if (at_start) {
            size_t pos = token.find('=');
            bool looks_like_assignment = pos != string::npos && pos > 0 &&
                                         token.find_first_of(" \t\n\r\"'") == string::npos;
            if (looks_like_assignment) {
                string name = token.substr(0, pos);
                if (!name.empty() && (isalpha((unsigned char)name[0]) || name[0] == '_')) {
                    setenv(name.c_str(), token.substr(pos + 1).c_str(), 1);
                    continue;
                }
            }
            at_start = false;
        }
        filtered.push_back(token);
    }
    argv.swap(filtered);
}

bool is_builtin_command(const string& cmd) {
    for (const auto& b : builtin_names) {
        if (cmd == b) return true;
    }
    return false;
}

int run_builtin_command(const vector<string>& argv);
void load_rc_file(const string& path, bool top_level);

int run_builtin_command(const vector<string>& argv) {
    if (argv.empty()) return 0;
    const string& cmd = argv[0];

    if (cmd == "cd") {
        string target = argv.size() > 1 ? expand_environment_variables(argv[1]) : string(getenv("HOME") ? getenv("HOME") : ".");
        string previous = get_current_dir();
        if (chdir(target.c_str()) != 0) {
            perror("cd");
            return 1;
        }
        setenv("OLDPWD", previous.c_str(), 1);
        setenv("PWD", get_current_dir().c_str(), 1);
        return 0;
    }
    if (cmd == "export") {
        for (size_t i = 1; i < argv.size(); ++i) {
            string item = expand_environment_variables(argv[i]);
            size_t equals = item.find('=');
            if (equals != string::npos) {
                setenv(item.substr(0, equals).c_str(), item.substr(equals + 1).c_str(), 1);
            }
        }
        return 0;
    }
    if (cmd == "unset") {
        for (size_t i = 1; i < argv.size(); ++i) unsetenv(argv[i].c_str());
        return 0;
    }
    if (cmd == "alias") {
        if (argv.size() == 1) {
            for (const auto& kv : aliases) {
                cout << "alias " << kv.first << "='" << kv.second << "'\n";
            }
            return 0;
        }
        for (size_t i = 1; i < argv.size(); ++i) {
            const string& item = argv[i];
            size_t eq = item.find('=');
            if (eq == string::npos) {
                auto it = aliases.find(item);
                if (it != aliases.end()) {
                    cout << "alias " << it->first << "='" << it->second << "'\n";
                } else {
                    cout << "alias: " << item << ": not found\n";
                }
                continue;
            }
            string name = item.substr(0, eq);
            string value = item.substr(eq + 1);
            if (value.size() >= 2 &&
                ((value.front() == '"' && value.back() == '"') ||
                 (value.front() == '\'' && value.back() == '\''))) {
                value = value.substr(1, value.size() - 2);
            }
            if (!name.empty()) aliases[name] = value;
        }
        return 0;
    }
    if (cmd == "unalias") {
        for (size_t i = 1; i < argv.size(); ++i) aliases.erase(argv[i]);
        return 0;
    }
    if (cmd == "source" || cmd == ".") {
        if (argv.size() < 2) return 1;
        string file = expand_environment_variables(argv[1]);
        load_rc_file(file, false);
        return 0;
    }
    if (cmd == "exit") {
        cout << "Bye from Humanix dish\n";
        return 0;
    }
    if (cmd == "dishhelp") {
        cout << "dish (Humanix Shell) - fast native C++ shell\n";
        cout << "Built-ins: cd, export, unset, alias, unalias, source, exit, history, jobs, dishhelp\n";
        return 0;
    }
    if (cmd == "history") {
        for (size_t i = 0; i < history.size(); ++i) {
            cout << "  " << (i + 1) << "  " << history[i] << "\n";
        }
        return 0;
    }
    if (cmd == "jobs") {
        cout << "Last background PID: " << last_background_pid << "\n";
        return 0;
    }
    return 0;
}

void load_rc_file(const string& path, bool top_level) {
    ifstream rc(path);
    if (!rc) {
        if (top_level) return;
        perror("source");
        return;
    }

    string line;
    while (getline(rc, line)) {
        size_t start = line.find_first_not_of(" \t");
        if (start == string::npos) continue;
        if (line[start] == '#') continue;

        vector<string> tokens = split_shell_tokens(line);
        if (tokens.empty()) continue;

        if (tokens[0] == "export") {
            for (size_t i = 1; i < tokens.size(); ++i) {
                string item = expand_environment_variables(tokens[i]);
                size_t eq = item.find('=');
                if (eq != string::npos) {
                    setenv(item.substr(0, eq).c_str(), item.substr(eq + 1).c_str(), 1);
                }
            }
            continue;
        }
        if (tokens[0] == "alias") {
            for (size_t i = 1; i < tokens.size(); ++i) {
                const string& item = tokens[i];
                size_t eq = item.find('=');
                if (eq == string::npos) continue;
                string name = item.substr(0, eq);
                string value = item.substr(eq + 1);
                if (value.size() >= 2 &&
                    ((value.front() == '"' && value.back() == '"') ||
                     (value.front() == '\'' && value.back() == '\''))) {
                    value = value.substr(1, value.size() - 2);
                }
                if (!name.empty()) aliases[name] = value;
            }
            continue;
        }
        if (tokens[0] == "source" || tokens[0] == ".") {
            if (tokens.size() >= 2) {
                load_rc_file(expand_environment_variables(tokens[1]), false);
            }
            continue;
        }
    }
}

vector<Command> parse_commands(const vector<string>& tokens) {
    if (tokens.empty()) return {};

    vector<Command> commands;
    Command current;

    for (size_t i = 0; i < tokens.size(); ++i) {
        const string& token = tokens[i];
        if (token == "|") {
            commands.push_back(current);
            current = Command();
            continue;
        }
        if (token == ">" || token == ">>" || token == "<") {
            if (i + 1 >= tokens.size()) return {};
            string target = expand_environment_variables(tokens[++i]);
            if (token == ">") { current.output_file = target; current.append_output = false; }
            else if (token == ">>") { current.output_file = target; current.append_output = true; }
            else { current.input_file = target; }
            continue;
        }
        current.argv.push_back(expand_environment_variables(token));
    }
    if (!current.argv.empty() || !current.input_file.empty() || !current.output_file.empty()) {
        commands.push_back(current);
    }
    return commands;
}

pid_t shell_pgid = 0;

void give_terminal_to(pid_t pgid) {
    signal(SIGTTOU, SIG_IGN);
    tcsetpgrp(STDIN_FILENO, pgid);
    signal(SIGTTOU, SIG_IGN);
}

void execute_commands(const vector<Command>& commands, bool background) {
    if (commands.empty()) return;

    if (commands.size() == 1 && !commands[0].argv.empty() && is_builtin_command(commands[0].argv[0])) {
        if (commands[0].argv[0] == "exit") {
            restore_terminal();
            exit(0);
        }
        last_exit_status = run_builtin_command(commands[0].argv);
        return;
    }

    vector<pid_t> pids;
    pids.reserve(commands.size());
    int prev_read = -1;

    for (size_t i = 0; i < commands.size(); ++i) {
        int pipefd[2] = {-1, -1};
        if (i + 1 < commands.size() && pipe(pipefd) == -1) {
            perror("pipe");
            return;
        }

        pid_t pid = fork();
        if (pid == 0) {
            signal(SIGINT, SIG_DFL);
            signal(SIGQUIT, SIG_DFL);
            signal(SIGTSTP, SIG_DFL);
            signal(SIGTTIN, SIG_DFL);
            signal(SIGTTOU, SIG_DFL);

            pid_t child_pgid = getpid();
            setpgid(0, child_pgid);

            if (!background) {
                signal(SIGTTOU, SIG_IGN);
                tcsetpgrp(STDIN_FILENO, child_pgid);
                signal(SIGTTOU, SIG_DFL);
            }

            if (prev_read != -1) {
                dup2(prev_read, STDIN_FILENO);
                close(prev_read);
            }
            if (!commands[i].input_file.empty()) {
                int in_fd = open(commands[i].input_file.c_str(), O_RDONLY);
                if (in_fd >= 0) { dup2(in_fd, STDIN_FILENO); close(in_fd); }
            }
            if (i + 1 < commands.size()) {
                dup2(pipefd[1], STDOUT_FILENO);
                close(pipefd[0]);
                close(pipefd[1]);
            }
            if (!commands[i].output_file.empty()) {
                int flags = O_WRONLY | O_CREAT | (commands[i].append_output ? O_APPEND : O_TRUNC);
                int out_fd = open(commands[i].output_file.c_str(), flags, 0644);
                if (out_fd >= 0) { dup2(out_fd, STDOUT_FILENO); close(out_fd); }
            }

            vector<char*> argv_ptrs;
            argv_ptrs.reserve(commands[i].argv.size() + 1);
            for (const string& arg : commands[i].argv) {
                argv_ptrs.push_back(const_cast<char*>(arg.c_str()));
            }
            argv_ptrs.push_back(nullptr);

            if (!argv_ptrs.empty() && argv_ptrs[0] != nullptr) {
                if (strchr(argv_ptrs[0], '/') != nullptr) {
                    execv(argv_ptrs[0], argv_ptrs.data());
                    if (errno == ENOEXEC) {
                        vector<char*> bash_argv;
                        bash_argv.reserve(argv_ptrs.size() + 1);
                        bash_argv.push_back(const_cast<char*>("bash"));
                        for (const string& arg : commands[i].argv) {
                            bash_argv.push_back(const_cast<char*>(arg.c_str()));
                        }
                        bash_argv.push_back(nullptr);
                        execv("/bin/bash", bash_argv.data());
                    }
                } else {
                    execvp(argv_ptrs[0], argv_ptrs.data());
                }
                fprintf(stderr, "%s: %s\n", argv_ptrs[0], strerror(errno));
            }
            _exit(127);
        } else if (pid < 0) {
            perror("fork");
            if (i + 1 < commands.size()) { close(pipefd[0]); close(pipefd[1]); }
            if (prev_read != -1) close(prev_read);
            return;
        } else {
            pid_t child_pgid = pid;
            setpgid(pid, child_pgid);

            if (i == 0 && !background) {
                give_terminal_to(child_pgid);
            }

            if (prev_read != -1) close(prev_read);
            pids.push_back(pid);
            if (i + 1 < commands.size()) {
                prev_read = pipefd[0];
                close(pipefd[1]);
            }
        }
    }

    if (background) {
        cout << "[job started] " << pids.front() << "\n";
        last_background_pid = pids.front();
        return;
    }

    int status;
    for (pid_t pid : pids) {
        pid_t result;
        do {
            result = waitpid(pid, &status, WUNTRACED);
        } while (result == -1 && errno == EINTR);

        if (result != -1 && WIFEXITED(status)) {
            last_exit_status = WEXITSTATUS(status);
        } else if (result != -1 && WIFSIGNALED(status)) {
            last_exit_status = 128 + WTERMSIG(status);
        }
    }

    give_terminal_to(shell_pgid);
}

void execute_line(const string& input) {
    vector<string> tokens = split_shell_tokens(input);
    if (tokens.empty()) return;

    bool background = false;
    if (tokens.back() == "&") {
        background = true;
        tokens.pop_back();
    }

    apply_environment_assignments(tokens);
    if (tokens.empty()) return;

    if (!tokens.empty() && aliases.count(tokens[0])) {
        vector<string> expansion = split_shell_tokens(aliases[tokens[0]]);
        tokens.erase(tokens.begin());
        tokens.insert(tokens.begin(), expansion.begin(), expansion.end());
    }

    if (!tokens.empty() && (tokens[0] == "source" || tokens[0] == ".")) {
        if (tokens.size() >= 2) {
            load_rc_file(expand_environment_variables(tokens[1]), false);
        }
        return;
    }

    if (!tokens.empty() && is_builtin_command(tokens[0]) && tokens[0] != "exit") {
        last_exit_status = run_builtin_command(tokens);
        return;
    }

    vector<Command> commands = parse_commands(tokens);
    if (commands.empty()) return;

    execute_commands(commands, background);
}

int main() {
    setlocale(LC_ALL, "");
    install_shell_signal_handlers();

    shell_pgid = getpgrp();

    string home = getenv("HOME") ? getenv("HOME") : "/";
    load_rc_file(home + "/.dishrc", true);

    const string history_path = home + "/.dish_history";

    ifstream infile(history_path);
    string line;
    while (getline(infile, line)) {
        if (!line.empty()) history.push_back(line);
    }
    infile.close();

    if (tcgetattr(0, &orig_termios) == -1) return 1;
    termios_saved = true;

    give_terminal_to(shell_pgid);

    while (true) {
        string input;
        size_t cursor = 0;

        print_prompt();

        struct termios raw = orig_termios;
        raw.c_lflag &= ~(ICANON | ECHO);
        tcsetattr(0, TCSAFLUSH, &raw);

        char c;
        while (read(0, &c, 1) == 1) {
            if (c == 4) {
                if (input.empty()) { input = "exit"; break; }
            } else if (c == 3) {
                cout << "^C\n";
                input.clear();
                cursor = 0;
                print_prompt();
            } else if (c == '\n' || c == '\r') {
                redraw_line(input, input.size());
                cout << "\n";
                break;
            } else if (c == 127 || c == 8) {
                if (cursor > 0) {
                    size_t bytes_to_erase = 1;
                    while (cursor > bytes_to_erase &&
                           (static_cast<unsigned char>(input[cursor - bytes_to_erase]) & 0xC0) == 0x80) {
                        bytes_to_erase++;
                    }
                    input.erase(cursor - bytes_to_erase, bytes_to_erase);
                    cursor -= bytes_to_erase;

                    redraw_line(input, cursor);
                }
            } else if (c == '\t') {
                string sug = find_suggestion(input);
                if (!sug.empty()) {
                    input += sug;
                    cursor = input.size();
                    redraw_line(input, cursor);
                }
            } else if (c == '\x1b') {
                string seq;
                seq += c;
                if (read(0, &c, 1) == 1) {
                    seq += c;
                    if (c == '[') {
                        while (read(0, &c, 1) == 1) {
                            seq += c;
                            if ((unsigned char)c >= 0x40 && (unsigned char)c <= 0x7E && c != '[') break;
                        }
                    }
                }

                if (seq == "\x1b[A") {
                    if (!history.empty()) {
                        if (history_idx == -1) {
                            saved_input = input;
                            history_idx = static_cast<int>(history.size()) - 1;
                        } else if (history_idx > 0) {
                            history_idx--;
                        }
                        input = history[history_idx];
                        cursor = input.size();
                        redraw_line(input, cursor);
                    }
                } else if (seq == "\x1b[B") {
                    if (history_idx != -1) {
                        if (history_idx < static_cast<int>(history.size()) - 1) {
                            history_idx++;
                            input = history[history_idx];
                        } else {
                            history_idx = -1;
                            input = saved_input;
                        }
                        cursor = input.size();
                        redraw_line(input, cursor);
                    }
                } else if (seq == "\x1b[D") {
                    if (cursor > 0) {
                        size_t step = 1;
                        while (cursor > step && (static_cast<unsigned char>(input[cursor - step]) & 0xC0) == 0x80) step++;
                        cursor -= step;
                        redraw_line(input, cursor);
                    }
                } else if (seq == "\x1b[C") {
                    if (cursor < input.size()) {
                        size_t step = get_utf8_char_len(static_cast<unsigned char>(input[cursor]));
                        cursor += step;
                        redraw_line(input, cursor);
                    } else {
                        string sug = find_suggestion(input);
                        if (!sug.empty()) {
                            input += sug;
                            cursor = input.size();
                            redraw_line(input, cursor);
                        }
                    }
                }
            } else if ((unsigned char)c >= 32) {
                string utf8_char(1, c);
                size_t char_len = get_utf8_char_len(static_cast<unsigned char>(c));
                for (size_t i = 1; i < char_len; ++i) {
                    char next_c;
                    if (read(0, &next_c, 1) == 1) utf8_char += next_c;
                }

                if (cursor == input.size()) input += utf8_char;
                else input.insert(cursor, utf8_char);
                cursor += utf8_char.size();

                redraw_line(input, cursor);
            }
        }

        tcsetattr(0, TCSAFLUSH, &orig_termios);

        if (input == "exit") break;
        if (input.empty()) continue;

        if (history.empty() || history.back() != input) {
            if (history.size() >= 1000) history.erase(history.begin());
            history.push_back(input);

            ofstream append_hist(history_path, ios::app);
            if (append_hist.is_open()) {
                append_hist << input << "\n";
            }
        }

        history_idx = -1;
        saved_input.clear();

        execute_line(input);
    }

    ofstream outfile(history_path);
    for (const string& entry : history) outfile << entry << "\n";
    outfile.close();

    return 0;
}