; Octopus Windows 安装脚本 (NSIS 3)
; 用法: makensis /DVERSION=x.y.z /DBIN_PATH=octopus-desktop.exe octopus.nsi

!ifndef VERSION
  !define VERSION "dev"
!endif
!ifndef BIN_PATH
  !define BIN_PATH "octopus-desktop.exe"
!endif
!ifndef LICENSE_PATH
  !define LICENSE_PATH "LICENSE"
!endif
!ifndef OUT_PATH
  !define OUT_PATH "octopus-setup-${VERSION}.exe"
!endif

Name "Octopus ${VERSION}"
OutFile "${OUT_PATH}"
Unicode True
InstallDir "$PROGRAMFILES64\Octopus"
InstallDirRegKey HKCU "Software\Octopus" "InstallDir"
RequestExecutionLevel admin
SetCompressor /SOLID lzma

; 界面
Page directory
Page components
Page instfiles
UninstPage uninstConfirm
UninstPage instfiles

Section "Octopus 主程序" SEC_MAIN
  SectionIn RO
  SetOutPath "$INSTDIR"
  File "${BIN_PATH}"
  File "${LICENSE_PATH}"

  ; 卸载器
  WriteUninstaller "$INSTDIR\uninstall.exe"

  ; 开始菜单
  CreateDirectory "$SMPROGRAMS\Octopus"
  CreateShortcut "$SMPROGRAMS\Octopus\Octopus.lnk" "$INSTDIR\octopus-desktop.exe"
  CreateShortcut "$SMPROGRAMS\Octopus\卸载 Octopus.lnk" "$INSTDIR\uninstall.exe"
  ; 桌面快捷方式
  CreateShortcut "$DESKTOP\Octopus.lnk" "$INSTDIR\octopus-desktop.exe"

  ; 注册卸载信息
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\Octopus" "DisplayName" "Octopus"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\Octopus" "DisplayVersion" "${VERSION}"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\Octopus" "Publisher" "hureru"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\Octopus" "DisplayIcon" "$INSTDIR\octopus-desktop.exe"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\Octopus" "UninstallString" "$INSTDIR\uninstall.exe"
  WriteRegDWORD HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\Octopus" "NoModify" 1
  WriteRegDWORD HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\Octopus" "NoRepair" 1
  WriteRegStr HKCU "Software\Octopus" "InstallDir" "$INSTDIR"
SectionEnd

Section "开机自启动" SEC_AUTOSTART
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Run" "Octopus" '"$INSTDIR\octopus-desktop.exe" desktop'
SectionEnd

Section "Uninstall"
  ; 删除快捷方式
  Delete "$DESKTOP\Octopus.lnk"
  Delete "$SMPROGRAMS\Octopus\Octopus.lnk"
  Delete "$SMPROGRAMS\Octopus\卸载 Octopus.lnk"
  RMDir "$SMPROGRAMS\Octopus"

  ; 移除开机自启（若存在）
  DeleteRegValue HKCU "Software\Microsoft\Windows\CurrentVersion\Run" "Octopus"

  ; 删除文件（保留用户数据目录 %APPDATA%\Octopus）
  Delete "$INSTDIR\octopus-desktop.exe"
  Delete "$INSTDIR\LICENSE"
  Delete "$INSTDIR\uninstall.exe"
  RMDir "$INSTDIR"

  ; 卸载信息
  DeleteRegKey HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\Octopus"
  DeleteRegKey HKCU "Software\Octopus"
SectionEnd
