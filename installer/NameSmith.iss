; NameSmith installer (Inno Setup 6)
; Build:  ISCC.exe /DAppVersion=1.0.0 installer\NameSmith.iss
; (dev builds: /DAppVersion=dev-abc1234 /DNumVersion=0.0.0)
; Needs dist\NameSmith.exe (see README "직접 빌드하기").

#ifndef AppVersion
  #define AppVersion "1.0.0"
#endif
#ifndef NumVersion
  #define NumVersion AppVersion
#endif
#define AppName "NameSmith"
#define AppExe "NameSmith.exe"
#define AppURL "https://github.com/game-addicted-man/NameSmith"

[Setup]
AppId={{6E2B9F4C-6A51-4F4B-9C8A-3D2E7B1A5C90}
AppName={#AppName}
AppVersion={#AppVersion}
AppVerName={#AppName} {#AppVersion}
AppPublisher=game-addicted-man
AppPublisherURL={#AppURL}
AppSupportURL={#AppURL}/issues
AppUpdatesURL={#AppURL}/releases
AppComments=파일 이름 일괄 변경기 - 번호 붙이기, 찾아 바꾸기, 날짜 넣기
DefaultDirName={autopf}\{#AppName}
DefaultGroupName={#AppName}
DisableProgramGroupPage=yes
; Installs for the current user without admin rights (no UAC prompt).
; "/ALLUSERS" on the command line installs for everyone instead.
PrivilegesRequired=lowest
PrivilegesRequiredOverridesAllowed=commandline
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
MinVersion=10.0
WizardStyle=modern
ShowLanguageDialog=auto
SetupIconFile=..\build\icon.ico
UninstallDisplayIcon={app}\{#AppExe}
UninstallDisplayName={#AppName} - 파일 이름 일괄 변경기
OutputDir=..\dist
OutputBaseFilename=NameSmith-Setup-{#AppVersion}
Compression=lzma2/ultra64
SolidCompression=yes
CloseApplications=yes
RestartApplications=no
ChangesAssociations=yes
VersionInfoVersion={#NumVersion}
VersionInfoProductName={#AppName}
VersionInfoDescription={#AppName} 설치 프로그램
VersionInfoCompany=game-addicted-man
VersionInfoCopyright=(c) 2026 Jonghoon Lee, MIT License

[Languages]
Name: "korean"; MessagesFile: "compiler:Languages\Korean.isl"
Name: "english"; MessagesFile: "compiler:Default.isl"

[CustomMessages]
korean.TaskDesktop=바탕 화면에 바로 가기 만들기
korean.TaskFolderMenu=폴더를 오른쪽 클릭하면 "NameSmith로 이름 바꾸기" 보이기
korean.TaskSendTo=파일을 오른쪽 클릭 > 보내기 메뉴에 NameSmith 넣기 (여러 파일을 한 번에)
korean.Extra=추가 기능:
korean.FolderVerb=NameSmith로 이름 바꾸기
korean.RunNow=NameSmith 바로 실행
english.TaskDesktop=Create a desktop shortcut
english.TaskFolderMenu=Add "Rename with NameSmith" when right-clicking a folder
english.TaskSendTo=Add NameSmith to the "Send to" menu (many files at once)
english.Extra=Extras:
english.FolderVerb=Rename with NameSmith
english.RunNow=Start NameSmith now

[Tasks]
Name: "desktopicon"; Description: "{cm:TaskDesktop}"; GroupDescription: "{cm:Extra}"
Name: "foldermenu"; Description: "{cm:TaskFolderMenu}"; GroupDescription: "{cm:Extra}"
Name: "sendto"; Description: "{cm:TaskSendTo}"; GroupDescription: "{cm:Extra}"

[Files]
Source: "..\dist\{#AppExe}"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\LICENSE"; DestDir: "{app}"; DestName: "LICENSE.txt"; Flags: ignoreversion

[Icons]
Name: "{autoprograms}\{#AppName}"; Filename: "{app}\{#AppExe}"; Comment: "파일 이름 일괄 변경기"
Name: "{autodesktop}\{#AppName}"; Filename: "{app}\{#AppExe}"; Tasks: desktopicon
Name: "{usersendto}\{#AppName}"; Filename: "{app}\{#AppExe}"; Tasks: sendto

[Registry]
; Right-click on a folder
Root: HKA; Subkey: "Software\Classes\Directory\shell\NameSmith"; ValueType: string; ValueName: ""; ValueData: "{cm:FolderVerb}"; Flags: uninsdeletekey; Tasks: foldermenu
Root: HKA; Subkey: "Software\Classes\Directory\shell\NameSmith"; ValueType: string; ValueName: "Icon"; ValueData: """{app}\{#AppExe}"",0"; Tasks: foldermenu
Root: HKA; Subkey: "Software\Classes\Directory\shell\NameSmith\command"; ValueType: string; ValueName: ""; ValueData: """{app}\{#AppExe}"" ""%1"""; Tasks: foldermenu
; Right-click on empty space inside a folder
Root: HKA; Subkey: "Software\Classes\Directory\Background\shell\NameSmith"; ValueType: string; ValueName: ""; ValueData: "{cm:FolderVerb}"; Flags: uninsdeletekey; Tasks: foldermenu
Root: HKA; Subkey: "Software\Classes\Directory\Background\shell\NameSmith"; ValueType: string; ValueName: "Icon"; ValueData: """{app}\{#AppExe}"",0"; Tasks: foldermenu
Root: HKA; Subkey: "Software\Classes\Directory\Background\shell\NameSmith\command"; ValueType: string; ValueName: ""; ValueData: """{app}\{#AppExe}"" ""%V"""; Tasks: foldermenu

[Run]
Filename: "{app}\{#AppExe}"; Description: "{cm:RunNow}"; Flags: nowait postinstall skipifsilent
