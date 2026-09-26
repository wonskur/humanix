#!/bin/bash
set -euo pipefail

if [ "$(id -u)" -ne 0 ]; then
    echo "[!] This script must be executed with sudo/root privileges."
    exit 1
fi

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CHROOT_STAGING="${PROJECT_ROOT}/config/includes.chroot/tmp/src"

cleanup() {
    local exit_code=$?
    echo "[*] Triggered cleanup trap..."
    rm -rf "${CHROOT_STAGING}"
    if [ ${exit_code} -ne 0 ]; then
        echo "[!] Build failed. Cleaning up stale live-build mounts..."
        lb clean --purge || true
    fi
    exit ${exit_code}
}
trap cleanup EXIT INT TERM

echo "[+] Syncing local source trees to live-build staging area..."
mkdir -p "${CHROOT_STAGING}"
rsync -a --delete --exclude='.git' "${PROJECT_ROOT}/src/" "${CHROOT_STAGING}/"

echo "[+] Initializing live-build configuration..."
lb clean
lb config

echo "[+] Executing ISO build pipeline..."
lb build

# Переименование в брендированный файл
RAW_ISO=$(ls live-image-amd64*.iso 2>/dev/null | head -n 1 || true)
if [ -n "${RAW_ISO}" ]; then
    mv "${RAW_ISO}" "humanix-live-amd64.iso"
    echo "[✓] Output generated: humanix-live-amd64.iso"
fi
