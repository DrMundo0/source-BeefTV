Unicode true
!include "MUI2.nsh"
!include "LogicLib.nsh"
!include "StrFunc.nsh"
!include "TextFunc.nsh"
!include "FileFunc.nsh"
${StrStr}
${UnStrStr}
Name "BeefTV"
OutFile "${OUTPUT}"
RequestExecutionLevel user
SetCompressor /SOLID lzma
InstallDir "$LOCALAPPDATA\Programs\BeefTV"
# Upgrades default to the previous location; the user can still choose another folder.
InstallDirRegKey HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\BeefTV" "InstallLocation"
Var StageDir
Var BackupDir
Var CleanDir
Var InstallParent
VIProductVersion "${VERSION}.0"
VIAddVersionKey "ProductName" "BeefTV"
VIAddVersionKey "FileVersion" "${VERSION}"
VIAddVersionKey "FileDescription" "BeefTV Setup"
!define MUI_ABORTWARNING
!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_COMPONENTS
!define MUI_PAGE_CUSTOMFUNCTION_LEAVE DirectoryPageLeave
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "English"
!insertmacro MUI_LANGUAGE "SimpChinese"

LangString BEEFTV_DIR_ROOT ${LANG_ENGLISH} "Choose a folder on a drive, not the drive root itself."
LangString BEEFTV_DIR_ROOT ${LANG_SIMPCHINESE} "请选择硬盘上的一个文件夹，而不是盘符根目录。"
LangString BEEFTV_DIR_FOREIGN ${LANG_ENGLISH} "This folder already holds other files and is not a BeefTV installation. Choose an empty folder or the current BeefTV location."
LangString BEEFTV_DIR_FOREIGN ${LANG_SIMPCHINESE} "这个文件夹里已有其他文件，不是 BeefTV 安装位置。请选择一个空文件夹，或当前的 BeefTV 安装位置。"
LangString BEEFTV_DIR_WRITE ${LANG_ENGLISH} "BeefTV cannot write here. Administrator rights are required, so choose another folder."
LangString BEEFTV_DIR_WRITE ${LANG_SIMPCHINESE} "需要管理员权限，请选择其他文件夹。"

# Validates the chosen folder, then guarantees a dedicated BeefTV subfolder.
# The checks run before appending, so a directory that already holds user data
# is refused instead of becoming a parent. Sets $0 to 0 when valid, or
# 1 (drive root), 2 (foreign folder), 3 (not writable).
Function PrepareInstallDir
  StrCpy $0 0
  ${If} $INSTDIR == ""
    StrCpy $INSTDIR "$LOCALAPPDATA\Programs\BeefTV"
    Return
  ${EndIf}
  # Drop trailing separators so the last component is a real name.
  StrCpy $1 "$INSTDIR"
  PrepareInstallDir_trim:
    StrCpy $2 "$1" 1 -1
    ${If} $2 == "\"
    ${OrIf} $2 == "/"
      StrCpy $3 "$1" -1
      ${If} $3 != ""
        StrCpy $1 "$3"
        Goto PrepareInstallDir_trim
      ${EndIf}
    ${EndIf}
  # A drive or share root is not a dedicated application folder.
  ${GetRoot} "$1" $2
  ${If} $2 == "$1"
    StrCpy $0 1
    Return
  ${EndIf}
  ${If} ${FileExists} "$1\*.*"
    # An existing folder is only reused when it is a previous BeefTV install.
    ${IfNot} ${FileExists} "$1\.installed-root-files.txt"
      StrCpy $3 ""
      FindFirst $2 $4 "$1\*"
      PrepareInstallDir_scan:
        ${If} $4 != ""
        ${AndIf} $4 != "."
        ${AndIf} $4 != ".."
          StrCpy $3 "1"
        ${EndIf}
        ${If} $4 != ""
          FindNext $2 $4
          Goto PrepareInstallDir_scan
        ${EndIf}
      FindClose $2
      ${If} $3 != ""
        StrCpy $0 2
        Return
      ${EndIf}
    ${EndIf}
  ${ElseIf} ${FileExists} "$1"
    StrCpy $0 2
    Return
  ${EndIf}
  # The installer runs as the current user, so Program Files and other
  # administrator-only folders must be rejected before anything is written.
  CreateDirectory "$1"
  ClearErrors
  FileOpen $2 "$1\.beef-tv-write-test" w
  ${If} ${Errors}
    StrCpy $0 3
    Return
  ${EndIf}
  FileClose $2
  Delete "$1\.beef-tv-write-test"
  # Always install into a folder named BeefTV so the payload never mixes with
  # files the user picked this folder for.
  ${GetFileName} "$1" $2
  ${If} $2 == "BeefTV"
    StrCpy $INSTDIR "$1"
  ${Else}
    StrCpy $INSTDIR "$1\BeefTV"
  ${EndIf}
FunctionEnd

Function ShowInstallDirError
  ${If} $0 == 1
    MessageBox MB_OK|MB_ICONSTOP "$(BEEFTV_DIR_ROOT)" /SD IDOK
  ${ElseIf} $0 == 2
    MessageBox MB_OK|MB_ICONSTOP "$(BEEFTV_DIR_FOREIGN)" /SD IDOK
  ${ElseIf} $0 == 3
    MessageBox MB_OK|MB_ICONSTOP "$(BEEFTV_DIR_WRITE)" /SD IDOK
  ${EndIf}
FunctionEnd

Function DirectoryPageLeave
  Call PrepareInstallDir
  ${If} $0 != 0
    Call ShowInstallDirError
    Abort
  ${EndIf}
FunctionEnd

!macro RequireClosed PREFIX
Function ${PREFIX}RequireClosed
  nsExec::ExecToStack '"$SYSDIR\tasklist.exe" /FI "IMAGENAME eq BeefTV.exe" /NH'
  Pop $0
  Pop $1
!if "${PREFIX}" == "un."
  ${UnStrStr} $2 $1 "BeefTV.exe"
!else
  ${StrStr} $2 $1 "BeefTV.exe"
!endif
  ${If} $2 == ""
!if "${PREFIX}" == "un."
    ${UnStrStr} $2 $1 "beeftv.exe"
!else
    ${StrStr} $2 $1 "beeftv.exe"
!endif
  ${EndIf}
  ${If} $0 != 0
  ${OrIf} $2 != ""
    MessageBox MB_OK|MB_ICONEXCLAMATION "Please close BeefTV and its CLI before installing or uninstalling." /SD IDOK
    SetErrorLevel 2
    Abort
  ${EndIf}
FunctionEnd
!macroend
!insertmacro RequireClosed ""
!insertmacro RequireClosed "un."

# Clean only shipped root files and runtime directories. Unknown user files remain.
!macro RemovePayload PREFIX
Function ${PREFIX}RemovePayload
  FileOpen $0 "$CleanDir\.installed-root-files.txt" r
  ${If} $0 != ""
    loop_${PREFIX}:
      ClearErrors
      FileRead $0 $1
      IfErrors done_${PREFIX}
      ${TrimNewLines} $1 $1
!if "${PREFIX}" == "un."
      ${UnStrStr} $2 $1 "\"
      ${UnStrStr} $3 $1 "/"
      ${UnStrStr} $4 $1 ":"
      ${UnStrStr} $5 $1 ".."
!else
      ${StrStr} $2 $1 "\"
      ${StrStr} $3 $1 "/"
      ${StrStr} $4 $1 ":"
      ${StrStr} $5 $1 ".."
!endif
      ${If} $1 != ""
      ${AndIf} $2 == ""
      ${AndIf} $3 == ""
      ${AndIf} $4 == ""
      ${AndIf} $5 == ""
        Delete "$CleanDir\$1"
      ${EndIf}
      Goto loop_${PREFIX}
    done_${PREFIX}:
    FileClose $0
  ${EndIf}
  Delete "$CleanDir\.installed-root-files.txt"
  Delete "$CleanDir\Uninstall.exe"
  RMDir /r "$CleanDir\agent-host"
  RMDir /r "$CleanDir\plugin-packages"
  RMDir /r "$CleanDir\cli"
  RMDir /r "$CleanDir\media-runtime"
  RMDir "$CleanDir"
FunctionEnd
!macroend
!insertmacro RemovePayload ""
!insertmacro RemovePayload "un."

Function .onInit
  SetShellVarContext current
  Call RequireClosed
  # Interactive runs validate on the directory page; /D= needs the same checks here.
  ${If} ${Silent}
    Call PrepareInstallDir
    ${If} $0 != 0
      Call ShowInstallDirError
      SetErrorLevel 1
      Abort
    ${EndIf}
  ${EndIf}
FunctionEnd

Function un.onInit
  SetShellVarContext current
  Call un.RequireClosed
FunctionEnd

Section "BeefTV (required)" SEC_APP
  # Stage and backup must live on the same volume as $INSTDIR for Rename to work.
  ${GetParent} "$INSTDIR" $InstallParent
  StrCpy $0 "$InstallParent" 1 -1
  ${If} $0 != "\"
    StrCpy $InstallParent "$InstallParent\"
  ${EndIf}
  ClearErrors
  CreateDirectory "$InstallParent"
  GetTempFileName $StageDir "$InstallParent"
  IfErrors prepare_failed
  Delete "$StageDir"
  IfErrors prepare_failed
  GetTempFileName $BackupDir "$InstallParent"
  IfErrors prepare_failed
  Delete "$BackupDir"
  IfErrors prepare_failed
  Goto stage_ready
  prepare_failed:
  MessageBox MB_OK|MB_ICONSTOP "Cannot prepare the installation. Check available disk space and try again. Your existing installation is unchanged." /SD IDOK
  SetErrorLevel 1
  Abort
  stage_ready:
  ClearErrors
  SetOutPath "$StageDir"
  IfErrors prepare_failed
  File /r "${PAYLOAD}\*"
  File /oname=.installed-root-files.txt "${ROOTFILES}"
  WriteUninstaller "$StageDir\Uninstall.exe"
  ${If} ${Errors}
    SetOutPath "$InstallParent"
    RMDir /r "$StageDir"
    MessageBox MB_OK|MB_ICONSTOP "Installation failed. Please run the installer again. Your projects have been preserved." /SD IDOK
    SetErrorLevel 1
    Abort
  ${EndIf}
  # Directory renames stay on the same volume. No old payload is removed until
  # all new files have been extracted successfully.
  SetOutPath "$InstallParent"
  ClearErrors
  IfFileExists "$INSTDIR\*.*" 0 install_new
  Rename "$INSTDIR" "$BackupDir"
  IfErrors install_failed
  install_new:
  Rename "$StageDir" "$INSTDIR"
  IfErrors install_restore
  StrCpy $CleanDir "$BackupDir"
  Call RemovePayload
  Goto install_registered
  install_restore:
  Rename "$BackupDir" "$INSTDIR"
  install_failed:
  RMDir /r "$StageDir"
  MessageBox MB_OK|MB_ICONSTOP "Installation could not replace the application. Close programs using BeefTV and try again. The previous application is kept at $INSTDIR or $BackupDir." /SD IDOK
  SetErrorLevel 1
  Abort
  install_registered:
  CreateDirectory "$SMPROGRAMS\BeefTV"
  CreateShortcut "$SMPROGRAMS\BeefTV\BeefTV.lnk" "$INSTDIR\BeefTV.exe"
  CreateShortcut "$SMPROGRAMS\BeefTV\Uninstall.lnk" "$INSTDIR\Uninstall.exe"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\BeefTV" "DisplayName" "BeefTV"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\BeefTV" "DisplayVersion" "${VERSION}"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\BeefTV" "UninstallString" '$\"$INSTDIR\Uninstall.exe$\"'
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\BeefTV" "QuietUninstallString" '$\"$INSTDIR\Uninstall.exe$\" /S'
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\BeefTV" "InstallLocation" "$INSTDIR"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\BeefTV" "DisplayIcon" "$INSTDIR\BeefTV.exe"
  WriteRegDWORD HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\BeefTV" "NoModify" 1
  WriteRegDWORD HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\BeefTV" "NoRepair" 1
SectionEnd

Section /o "Desktop shortcut" SEC_DESKTOP
  CreateShortcut "$DESKTOP\BeefTV.lnk" "$INSTDIR\BeefTV.exe"
SectionEnd

Section "Uninstall"
  # NSIS sets $INSTDIR to the uninstaller's folder. Keep the working directory
  # out of it so the empty install folder can be removed.
  ${GetParent} "$INSTDIR" $0
  StrCpy $1 "$0" 1 -1
  ${If} $1 != "\"
    StrCpy $0 "$0\"
  ${EndIf}
  SetOutPath "$0"
  StrCpy $CleanDir "$INSTDIR"
  Call un.RemovePayload
  Delete "$DESKTOP\BeefTV.lnk"
  Delete "$SMPROGRAMS\BeefTV\BeefTV.lnk"
  Delete "$SMPROGRAMS\BeefTV\Uninstall.lnk"
  RMDir "$SMPROGRAMS\BeefTV"
  DeleteRegKey HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\BeefTV"
SectionEnd
