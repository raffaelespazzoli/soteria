@echo off
REM Post-install setup for Windows golden image capture.
REM Runs at the end of Windows Setup (C:\Windows\Setup\Scripts\SetupComplete.cmd).
REM
REM Guest tools are NOT installed here. build-windows-images-locally.sh leaves
REM the VM running with the virtio-win CD attached so you can install them
REM from the desktop, then ACPI-stop to capture the qcow2.

REM Disable Windows Firewall
netsh advfirewall set allprofiles state off

REM Prevent Windows 11 Device Encryption (BitLocker). A TPM (KubeVirt
REM windows.11 preference) otherwise encrypts C: and offline IP rewrite
REM cannot mount NTFS (virt-inspector prompts for a recovery key).
reg add HKLM\SYSTEM\CurrentControlSet\Control\BitLocker /v PreventDeviceEncryption /t REG_DWORD /d 1 /f
manage-bde -off C: >nul 2>&1

REM Shutdown so the local builder knows unattended setup is finished.
shutdown /s /t 30 /f
