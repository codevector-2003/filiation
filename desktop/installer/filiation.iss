; The Windows installer for Filiation: the desktop app and the fil command,
; installed for the current user, with no administrator rights (D37).
;
; Built by CI with Inno Setup 6:
;
;   ISCC.exe /DAppVersion=0.4.2 /DSourceDir=..\..\dist\windows filiation.iss
;
; SourceDir holds Filiation.exe and fil.exe, built for 64-bit Windows. The
; result is Output\Filiation-Setup-<version>-windows-amd64.exe.

#ifndef AppVersion
  #define AppVersion "0.0.0-dev"
#endif
#ifndef SourceDir
  #define SourceDir "build"
#endif

[Setup]
; Never change AppId: it is how a newer installer finds the old version to
; replace, and how Windows lists the app under Apps.
AppId={{57025EA5-61C3-4627-B1C3-3D79EC36DB3E}
AppName=Filiation
AppVersion={#AppVersion}
AppVerName=Filiation {#AppVersion}
AppPublisher=Filiation
AppPublisherURL=https://github.com/codevector-2003/filiation
AppSupportURL=https://github.com/codevector-2003/filiation/issues
AppUpdatesURL=https://github.com/codevector-2003/filiation/releases
; Per user, where the app has always been installed: assistants connected to
; fil keep the same path to it across upgrades.
PrivilegesRequired=lowest
DefaultDirName={localappdata}\Programs\Filiation
DisableProgramGroupPage=yes
DisableReadyPage=yes
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
WizardStyle=modern
SetupIconFile=filiation.ico
UninstallDisplayIcon={app}\filiation.ico
UninstallDisplayName=Filiation
OutputDir=Output
OutputBaseFilename=Filiation-Setup-{#AppVersion}-windows-amd64
Compression=lzma2/max
SolidCompression=yes
; Filiation, or an assistant's fil, may be running: offer to close them, as
; the Restart Manager allows, rather than fail on a file in use.
CloseApplications=yes
RestartApplications=no
; Adding fil to PATH changes the environment other programs read.
ChangesEnvironment=yes

[Messages]
WelcomeLabel2=Filiation builds a library of the papers you read: give it one paper and it follows the references, keeps the open-access PDFs, and lets you search them, on your own computer.%n%nThis installs Filiation [ver] for you only. It needs no administrator rights.
FinishedLabel=Filiation is installed. Find it in the Start menu. When it opens, it asks where to keep your library.
SelectDirLabel3=Filiation will be installed in this folder. Your libraries live elsewhere, in a folder you choose when Filiation first opens.
SelectTasksLabel2=Choose what else Setup should do.

[Tasks]
Name: "path"; Description: "Add fil to my PATH, so a terminal and AI assistants such as Claude Code or Cursor can run it (recommended)"
Name: "desktopicon"; Description: "Create a desktop shortcut"; Flags: unchecked

[InstallDelete]
; Copies set aside by hand-made installs before there was an installer, when
; a running exe could only be renamed; nothing else in the folder is touched.
Type: files; Name: "{app}\*.old.exe"

[Files]
Source: "{#SourceDir}\Filiation.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#SourceDir}\fil.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "filiation.ico"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\LICENSE"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\NOTICE"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{autoprograms}\Filiation"; Filename: "{app}\Filiation.exe"; IconFilename: "{app}\filiation.ico"
Name: "{autodesktop}\Filiation"; Filename: "{app}\Filiation.exe"; IconFilename: "{app}\filiation.ico"; Tasks: desktopicon

[Run]
Filename: "{app}\Filiation.exe"; Description: "Open Filiation"; Flags: nowait postinstall skipifsilent

[Code]
const
  EnvKey = 'Environment';

// onPath says whether dir is one of the entries of the user's PATH.
function OnPath(Dir: string): Boolean;
var
  Path: string;
begin
  if not RegQueryStringValue(HKEY_CURRENT_USER, EnvKey, 'Path', Path) then
    Path := '';
  Result := Pos(';' + Uppercase(Dir) + ';', ';' + Uppercase(Path) + ';') > 0;
end;

// The user's PATH gains the install folder, once; the system PATH, which
// needs administrator rights, is never touched.
procedure AddToPath(Dir: string);
var
  Path: string;
begin
  if OnPath(Dir) then
    exit;
  if not RegQueryStringValue(HKEY_CURRENT_USER, EnvKey, 'Path', Path) then
    Path := '';
  if (Path <> '') and (Copy(Path, Length(Path), 1) <> ';') then
    Path := Path + ';';
  RegWriteExpandStringValue(HKEY_CURRENT_USER, EnvKey, 'Path', Path + Dir);
end;

procedure RemoveFromPath(Dir: string);
var
  Path: string;
  P: Integer;
begin
  if not RegQueryStringValue(HKEY_CURRENT_USER, EnvKey, 'Path', Path) then
    exit;
  Path := ';' + Path + ';';
  P := Pos(';' + Uppercase(Dir) + ';', Uppercase(Path));
  if P = 0 then
    exit;
  Delete(Path, P, Length(Dir) + 1);
  Path := Copy(Path, 2, Length(Path) - 2);
  RegWriteExpandStringValue(HKEY_CURRENT_USER, EnvKey, 'Path', Path);
end;

procedure CurStepChanged(CurStep: TSetupStep);
begin
  if (CurStep = ssPostInstall) and WizardIsTaskSelected('path') then
    AddToPath(ExpandConstant('{app}'));
end;

var
  RemoveSettings: Boolean;

// Uninstalling removes the program. Libraries are never touched; settings and
// the cache of downloaded data go only if the person says so.
function InitializeUninstall(): Boolean;
begin
  RemoveSettings := False;
  if not UninstallSilent then
    RemoveSettings := MsgBox('Filiation will be removed. Your libraries are kept: their papers, PDFs and notes stay where they are.' + #13#10#13#10 +
      'Also remove Filiation''s settings and its cache of downloaded data? Your list of libraries and your choices go; the libraries themselves stay.',
      mbConfirmation, MB_YESNO or MB_DEFBUTTON2) = IDYES;
  Result := True;
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
begin
  if CurUninstallStep = usPostUninstall then
  begin
    RemoveFromPath(ExpandConstant('{app}'));
    if RemoveSettings then
    begin
      // Settings: the list of libraries and the person's choices.
      DelTree(ExpandConstant('{userappdata}\filiation'), True, True, True);
      // Only what is known to be disposable: downloaded responses and
      // PDFium's compiled code. Anything else in that folder, such as a
      // database backup, is left alone.
      DelTree(ExpandConstant('{localappdata}\filiation\http'), True, True, True);
      DelTree(ExpandConstant('{localappdata}\filiation\pdfium'), True, True, True);
    end;
  end;
end;
