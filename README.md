# <img width="40" height="40" src="./docs/icon.svg" alt="Humanix logo" /> Humanix GNU/Linux

[![Linux](https://img.shields.io/badge/Linux-FCC624?style=for-the-badge&logo=linux&logoColor=black)](https://kernel.org)
[![Debian](https://img.shields.io/badge/Debian_Trixie-A81D33?style=for-the-badge&logo=debian&logoColor=white)](https://www.debian.org)
[![Shell](https://img.shields.io/badge/Shell-4EAA25?style=for-the-badge&logo=gnu-bash&logoColor=white)](https://www.gnu.org/software/bash/)
[![License](https://img.shields.io/badge/License-GPL--3.0-blue?style=for-the-badge)](./LICENSE)
[![Build](https://img.shields.io/github/actions/workflow/status/wonskur/Humanix/build-iso.yml?style=for-the-badge&label=Build%20ISO)](../../actions)

Humanix is a small Linux system. It is based on Debian Trixie.
It is made for daily use and for learning.
You can change a real Linux system and see how it works.

Humanix starts from a live ISO.
It has a small command shell, system tools, and an installer.

---

## Included Tools

- `dish` — the Humanix command shell.
- `humanix-doctor` — checks common system problems.
- `sys-sentinel` and `sentinel-ctl` — show system status.
- `humanix-installer` — installs Humanix to a disk.

---

## Get the ISO

ISO files are built by GitHub Actions.

1. Open the **Actions** tab.
2. Choose **Build Humanix Live ISO**.
3. A build started from the `main` branch is saved as a workflow artifact.

---

## Install Safely

Please read this before you install.

- The installer erases every partition on the selected disk.
- Try it in a virtual machine first.
- Or use an empty disk.
- Back up any files you need before installing.

---

## About This Project

Humanix is a personal project.
You can read and change its configuration and source code in this repository.