#!/bin/bash
set -e
if [[ $EUID -ne 0 ]]; then
   echo "Run this script with sudo: sudo ./before-start.sh"
   exit 1
fi
echo -e "\033[1;36m:: Humanix OS - Post-Install Setup\033[0m\n"
if ping -c 1 -W 3 1.1.1.1 &>/dev/null; then
    echo "Internet connection: OK"
else
    echo "Internet connection: OFFLINE"
    read -r -p "Open 'nmtui' to connect? [Y/n]: " answer
    case "${answer,,}" in
        n|no) echo "Network is required. Exiting."; exit 1 ;;
        *) nmtui ;;
    esac
fi
if ! ping -c 1 -W 3 1.1.1.1 &>/dev/null; then
    echo "Still no internet. Exiting."
    exit 1
fi
echo "Updating package lists..."
apt update
ask_step(){
    local prompt="$1"
    local default="${2:-y}"
    read -r -p "$prompt [Y/n]: " ans
    ans="${ans,,}"
    if [[ -z "$ans" ]]; then ans="$default"; fi
    [[ "$ans" == "y" || "$ans" == "yes" ]]
}
if ask_step "Install hardware firmware (WiFi/GPU/CPU microcode)?"; then
    apt install -y firmware-linux firmware-linux-nonfree intel-microcode amd64-microcode
fi
if ask_step "Install base X11 server and fonts?"; then
    apt install -y xorg xinit fonts-dejavu fonts-liberation
fi
if ask_step "Install audio server (PipeWire recommended)?"; then
    echo "  1) pipewire (modern)"
    echo "  2) pulseaudio (legacy)"
    echo "  3) none"
    read -r -p "> " audio
    case "$audio" in
        1|"") apt install -y pipewire pipewire-pulse wireplumber ;;
        2) apt install -y pulseaudio ;;
    esac
fi
if ask_step "Choose Desktop Environment or Window Manager?"; then
    echo "  1) KDE Plasma (recommended)"
    echo "  2) XFCE (lightweight)"
    echo "  3) GNOME"
    echo "  4) i3 (tiling WM)"
    echo "  5) Openbox"
    echo "  6) MATE"
    echo "  7) Cinnamon"
    echo "  8) LXQt"
    echo "  9) None"
    read -r -p "> " de
    case "$de" in
        1) apt install -y kde-plasma-desktop ;;
        2) apt install -y xfce4 xfce4-goodies ;;
        3) apt install -y gnome-core ;;
        4) apt install -y i3 ;;
        5) apt install -y openbox obconf ;;
        6) apt install -y mate-desktop-environment-core ;;
        7) apt install -y cinnamon-core ;;
        8) apt install -y lxqt-core ;;
    esac
fi
if ask_step "Install Display Manager (login screen)?"; then
    echo "  1) sddm (best for KDE/LXQt)"
    echo "  2) lightdm (best for XFCE/WMs)"
    echo "  3) gdm3 (best for GNOME)"
    echo "  4) none (boot to console / startx)"
    read -r -p "> " dm
    case "$dm" in
        1) apt install -y sddm ;;
        2) apt install -y lightdm ;;
        3) apt install -y gdm3 ;;
    esac
fi
if ask_step "Install GUI terminal emulator?"; then
    echo "  1) alacritty"
    echo "  2) kitty"
    echo "  3) xfce4-terminal"
    echo "  4) none"
    read -r -p "> " term
    case "$term" in
        1) apt install -y alacritty ;;
        2) apt install -y kitty ;;
        3) apt install -y xfce4-terminal ;;
    esac
fi
if ask_step "Install web browser?"; then
    echo "  1) firefox-esr"
    echo "  2) chromium"
    echo "  3) none"
    read -r -p "> " br
    case "$br" in
        1|"") apt install -y firefox-esr ;;
        2) apt install -y chromium ;;
    esac
fi
if ask_step "Install GUI file manager?"; then
    echo "  1) thunar"
    echo "  2) pcmanfm"
    echo "  3) nemo"
    echo "  4) none"
    read -r -p "> " fm
    case "$fm" in
        1) apt install -y thunar ;;
        2) apt install -y pcmanfm ;;
        3) apt install -y nemo ;;
    esac
fi
if ask_step "Install essential CLI utilities (git, curl, wget, htop, archives)?"; then
    apt install -y git curl wget htop unzip zip p7zip-full tar gzip
fi
if ask_step "Clean package cache?"; then
    apt autoremove -y
    apt clean
fi
echo -e "\n\033[1;32mSetup finished successfully!\033[0m"
if ask_step "Reboot system now?"; then
    reboot
fi