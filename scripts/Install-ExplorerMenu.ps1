<#
.SYNOPSIS
  Registers the "ZIPを再帰展開" command for .zip files in the current user's Explorer context menu.

.DESCRIPTION
  The registration is written only under HKCU:\Software\Classes, so administrator privileges
  are not required and no machine-wide association is changed. The shell command passes the
  selected archive path to recursive-unzip.exe. MultiSelectModel=Player asks Explorer to use
  one invocation for a multiple selection where supported by the Shell.
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $false)]
    [string]$ExecutablePath = (Join-Path $PSScriptRoot "..\recursive-unzip.exe")
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

try {
    $resolvedExecutable = (Resolve-Path -LiteralPath $ExecutablePath).Path
}
catch {
    throw "recursive-unzip.exe が見つかりません: $ExecutablePath"
}

if (-not $resolvedExecutable.EndsWith(".exe", [System.StringComparison]::OrdinalIgnoreCase)) {
    throw "Windows向けの .exe ファイルを指定してください: $resolvedExecutable"
}

$verbKey = "HKCU:\Software\Classes\SystemFileAssociations\.zip\shell\RecursiveUnzip"
$commandKey = Join-Path $verbKey "command"

New-Item -Path $verbKey -Force | Out-Null
Set-ItemProperty -Path $verbKey -Name "MUIVerb" -Value "ZIPを再帰展開"
Set-ItemProperty -Path $verbKey -Name "Icon" -Value ('"{0}",0' -f $resolvedExecutable)
Set-ItemProperty -Path $verbKey -Name "MultiSelectModel" -Value "Player"
Set-ItemProperty -Path $verbKey -Name "Position" -Value "Top"

New-Item -Path $commandKey -Force | Out-Null
$command = '"{0}" "%1"' -f $resolvedExecutable
Set-Item -Path $commandKey -Value $command

Write-Host "Explorer右クリックメニューを登録しました。"
Write-Host "対象: .zip"
Write-Host "コマンド: $command"
Write-Host "Windows 11では右クリック後の『その他のオプションを表示』内に表示される場合があります。"
