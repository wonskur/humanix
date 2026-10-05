#!/bin/bash

if ping -c 1 -W 3 1.1.1.1 &>/dev/null; then
    echo "INTERNET IS WORKING"
else
    echo "INTERNET IS NOT WORKING"
    read -r -p "OPEN \"nmtui\" to connect? [Y/n]: " answer
    case "${answer,,}" in
        y|yes|"") nmtui ;;
        n|no) echo "OK, exiting"; exit 1 ;;
        *) echo "I DONT KNOW THAT ANSWER!"; exit 1 ;;
    esac
fi

if ! ping -c 1 -W 3 1.1.1.1 &>/dev/null; then
    echo "STILL NO INTERNET"
    exit 1
fi

read -r -p "Run humanix-installer now? [Y/n]: " ans
if [[ "${ans,,}" != "n" ]]; then
    humanix-installer || exit 1
fi

echo "POST-INSTALL PACKAGE SETUP"
echo "These packages will be installed into the TARGET system."
echo "Mount the target root first (e.g. /mnt/humanix_target) if it is not mounted."

read -r -p "Target root path [/mnt/humanix_target]: " root
if [[ -z "$root" ]]; then root="/mnt/humanix_target"; fi

if [[ ! -d "$root" ]]; then
    echo "Target root $root does not exist."
    exit 1
fi

bind_chroot() {
    mount --bind /dev  "$root/dev"     || true
    mount --bind /dev/pts "$root/dev/pts" || true
    mount --bind /proc "$root/proc"    || true
    mount --bind /sys  "$root/sys"     || true
    mount --bind /run  "$root/run"     || true
}

in_chroot() {
    chroot "$root" "$@"
}

bind_chroot

read -r -p "Install firmware (wifi/gpu)? [Y/n]: " ans
if [[ "${ans,,}" != "n" ]]; then
    in_chroot apt update
    in_chroot apt install -y firmware-linux firmware-linux-nonfree
fi

read -r -p "Install X11 (xorg, xinit)? [Y/n]: " ans
if [[ "${ans,,}" != "n" ]]; then
    in_chroot apt install -y xorg xinit
fi

read -r -p "Install fonts? [Y/n]: " ans
if [[ "${ans,,}" != "n" ]]; then
    in_chroot apt install -y fonts-dejavu fonts-liberation
fi

read -r -p "Install audio server? [Y/n]: " ans
if [[ "${ans,,}" != "n" ]]; then
    echo "  1) pipewire"
    echo "  2) pulseaudio"
    echo "  3) none"
    read -r -p "> " audio
    case "$audio" in
        1) in_chroot apt install -y pipewire pipewire-pulse wireplumber ;;
        2) in_chroot apt install -y pulseaudio ;;
    esac
fi

read -r -p "Install NetworkManager applet? [Y/n]: " ans
if [[ "${ans,,}" != "n" ]]; then
    in_chroot apt install -y network-manager network-manager-gnome
fi

read -r -p "Install browser? [Y/n]: " ans
if [[ "${ans,,}" != "n" ]]; then
    echo "  1) firefox-esr"
    echo "  2) chromium"
    echo "  3) none"
    read -r -p "> " b
    case "$b" in
        1) in_chroot apt install -y firefox-esr ;;
        2) in_chroot apt install -y chromium ;;
    esac
fi

read -r -p "Install terminal emulator? [Y/n]: " ans
if [[ "${ans,,}" != "n" ]]; then
    echo "  1) xterm"
    echo "  2) alacritty"
    echo "  3) kitty"
    echo "  4) none"
    read -r -p "> " t
    case "$t" in
        1) in_chroot apt install -y xterm ;;
        2) in_chroot apt install -y alacritty ;;
        3) in_chroot apt install -y kitty ;;
    esac
fi

read -r -p "Install file manager? [Y/n]: " ans
if [[ "${ans,,}" != "n" ]]; then
    echo "  1) pcmanfm"
    echo "  2) thunar"
    echo "  3) nautilus"
    echo "  4) none"
    read -r -p "> " f
    case "$f" in
        1) in_chroot apt install -y pcmanfm ;;
        2) in_chroot apt install -y thunar ;;
        3) in_chroot apt install -y nautilus ;;
    esac
fi

read -r -p "Install text editor? [Y/n]: " ans
if [[ "${ans,,}" != "n" ]]; then
    echo "  1) vim"
    echo "  2) nano"
    echo "  3) emacs"
    echo "  4) none"
    read -r -p "> " e
    case "$e" in
        1) in_chroot apt install -y vim ;;
        2) in_chroot apt install -y nano ;;
        3) in_chroot apt install -y emacs ;;
    esac
fi

read -r -p "Install image viewer (feh)? [Y/n]: " ans
if [[ "${ans,,}" != "n" ]]; then
    in_chroot apt install -y feh
fi

read -r -p "Install archive tools? [Y/n]: " ans
if [[ "${ans,,}" != "n" ]]; then
    in_chroot apt install -y unzip zip p7zip-full tar gzip
fi

read -r -p "Install git? [Y/n]: " ans
if [[ "${ans,,}" != "n" ]]; then
    in_chroot apt install -y git
fi

read -r -p "Install curl and wget? [Y/n]: " ans
if [[ "${ans,,}" != "n" ]]; then
    in_chroot apt install -y curl wget
fi

read -r -p "Install htop? [Y/n]: " ans
if [[ "${ans,,}" != "n" ]]; then
    in_chroot apt install -y htop
fi

read -r -p "Choose DE/WM? [Y/n]: " ans
if [[ "${ans,,}" != "n" ]]; then
    echo "  1) none"
    echo "  2) openbox"
    echo "  3) i3"
    echo "  4) xfce"
    echo "  5) kde(Recommended for beginners)"
    echo "  6) gnome"
    echo "  7) mate"
    echo "  8) cinnamon"
    echo "  9) lxqt"
    echo " 10) lxde"
    read -r -p "> " d
    case "$d" in
        2) in_chroot apt install -y openbox ;;
        3) in_chroot apt install -y i3 ;;
        4) in_chroot apt install -y xfce4 xfce4-goodies ;;
        5) in_chroot apt install -y kde-plasma-desktop ;;
        6) in_chroot apt install -y gnome ;;
        7) in_chroot apt install -y mate-desktop-environment ;;
        8) in_chroot apt install -y cinnamon ;;
        9) in_chroot apt install -y lxqt ;;
        10) in_chroot apt install -y lxde ;;
    esac
fi

read -r -p "Choose DM? [Y/n]: " ans
if [[ "${ans,,}" != "n" ]]; then
    echo "  1) none (startx)"
    echo "  2) lightdm"
    echo "  3) sddm"
    echo "  4) gdm3"
    echo "  5) lxdm"
    read -r -p "> " m
    case "$m" in
        2) in_chroot apt install -y lightdm ;;
        3) in_chroot apt install -y sddm ;;
        4) in_chroot apt install -y gdm3 ;;
        5) in_chroot apt install -y lxdm ;;
    esac
fi

read -r -p "Clean apt cache inside target? [Y/n]: " ans
if [[ "${ans,,}" != "n" ]]; then
    in_chroot apt clean
    in_chroot apt autoremove -y
fi

read -r -p "Reboot now? [Y/n]: " ans
if [[ "${ans,,}" != "n" ]]; then
    umount -R "$root" 2>/dev/null || true
    reboot
fi

echo "DONE"