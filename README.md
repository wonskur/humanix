# Humanix GNU/Linux

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

![Humanix Logo](./docs/icon.svg)