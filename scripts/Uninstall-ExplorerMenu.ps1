<#
.SYNOPSIS
  Removes the current-user "ZIPを再帰展開" Explorer context-menu registration.
#>
[CmdletBinding()]
param()

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$verbKey = "HKCU:\Software\Classes\SystemFileAssociations\.zip\shell\RecursiveUnzip"
if (Test-Path -LiteralPath $verbKey) {
    Remove-Item -LiteralPath $verbKey -Recurse -Force
    Write-Host "Explorer右クリックメニューの登録を解除しました。"
}
else {
    Write-Host "解除対象の登録は見つかりませんでした。"
}
