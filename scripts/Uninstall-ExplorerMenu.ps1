<#
.SYNOPSIS
  現在のユーザーに登録した「ZIPを再帰展開」のExplorer右クリックメニューを解除します。

.DESCRIPTION
  Install-ExplorerMenu.ps1 が作成した専用verbだけを削除します。
  ZIPの既定アプリや、他の右クリックメニューは変更しません。
#>
[CmdletBinding()]
param()

# 未定義変数や削除時の失敗を検出し、意図しない状態を見逃さないようにする。
Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

# 登録スクリプトと同じ現在のユーザー専用キーだけを解除対象とする。
$verbKey = "HKCU:\Software\Classes\SystemFileAssociations\.zip\shell\RecursiveUnzip"
if (Test-Path -LiteralPath $verbKey) {
    # commandサブキーも含めて削除し、登録前の状態へ戻す。
    Remove-Item -LiteralPath $verbKey -Recurse -Force
    Write-Host "Explorer右クリックメニューの登録を解除しました。"
}
else {
    # 冪等に実行できるよう、登録がない場合もエラーではなく状況を通知する。
    Write-Host "解除対象の登録は見つかりませんでした。"
}
