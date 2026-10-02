# Humanix GNU/Linux 1.0

Humanix is a small Linux system based on Debian Trixie. It is made for daily use and for learning by changing a real Linux system.

Humanix starts from a live ISO. It includes a small command shell, system tools, and an installer.

## Included tools

- `dish` is the Humanix command shell.
- `humanix-doctor` checks common system problems.
- `sys-sentinel` and `sentinel-ctl` show system status.
- `humanix-installer` installs Humanix to a disk.

## Get the ISO

ISO files are built by GitHub Actions. Open the **Actions** tab and choose **Build Humanix Live ISO**. A build started from the `main` branch is available as a workflow artifact.

## Install safely

The installer erases every partition on the selected disk. Try it in a virtual machine first, or use an empty disk. Back up any files you need before installing.

Humanix is a personal project. You can read and change its configuration and source code in this repository.
