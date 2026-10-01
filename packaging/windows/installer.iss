; Inno Setup script for the Windows installer (decision 20: CI runs ISCC
; with the binaries from the build steps).
;
; The installer copies the agent and the tray, installs and starts the agent
; service, and registers the tray at login machine-wide under HKLM — on
; purpose (see the plan): the installer usually runs elevated as an
; administrator who is not the kiosk user, so an HKCU value would land in the
; administrator's hive and the tray would never start for the person at the
; machine. Each user can still turn the entry off for themselves in
; Settings > Apps > Startup, which writes a per-user StartupApproved\Run
; override.

#define AppName "Thai Smartcard"
#define AppExeName "thai-smartcard-agent.exe"
#define TrayExeName "thai-smartcard-tray.exe"

[Setup]
AppId={{8E2C0F2A-6E3B-4A5F-9C1D-0A5B7C8D9E0F}
AppName={#AppName}
AppVersion={#AppVersion}
AppPublisher=somprasongd
DefaultDirName={autopf}\ThaiSmartcard
DefaultGroupName={#AppName}
UninstallDisplayIcon={app}\{#TrayExeName}
OutputDir=..
OutputBaseFilename=thai-smartcard-setup-{#AppVersion}
Compression=lzma
SolidCompression=yes
ArchitecturesInstallIn64BitMode=x64compatible
PrivilegesRequired=admin
WizardStyle=modern

[Tasks]
Name: "traylogin"; Description: "เริ่ม tray ตอนล็อกอิน (ทุกผู้ใช้) / Start the tray at login (all users)"; GroupDescription: "Tray:"; Flags: checkedonce

[Files]
Source: "..\bin\thai-smartcard-agent.windows-amd64.exe"; DestDir: "{app}"; DestName: "{#AppExeName}"; Flags: ignoreversion
Source: "..\bin\thai-smartcard-tray.exe"; DestDir: "{app}"; DestName: "{#TrayExeName}"; Flags: ignoreversion

[Run]
; The installer installs and starts the agent service (decision 12).
Filename: "{app}\{#AppExeName}"; Parameters: "service install"; Flags: runhidden
Filename: "{app}\{#AppExeName}"; Parameters: "service start"; Flags: runhidden
Filename: "{app}\{#TrayExeName}"; Description: "เปิด tray / Launch the tray now"; Flags: nowait postinstall skipifsilent

[Registry]
Root: HKLM; Subkey: "SOFTWARE\Microsoft\Windows\CurrentVersion\Run"; ValueType: string; ValueName: "ThaiSmartcardTray"; ValueData: """{app}\{#TrayExeName}"""; Tasks: traylogin; Flags: uninsdeletevalue

[UninstallRun]
Filename: "{app}\{#AppExeName}"; Parameters: "service uninstall"; Flags: runhidden; RunOnceId: "SvcUninstall"
Filename: "net.exe"; Parameters: "stop ThaiSmartcardTray"; Flags: runhidden; RunOnceId: "TrayStop"
