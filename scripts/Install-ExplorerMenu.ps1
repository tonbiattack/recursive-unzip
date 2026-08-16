<#
.SYNOPSIS
  現在のユーザーのExplorer右クリックメニューへ「ZIPを再帰展開」を登録します。

.DESCRIPTION
  HKCU:\Software\Classes 配下だけを変更するため、管理者権限は不要であり、
  マシン全体の関連付けも変更しません。選択したZIPのパスを recursive-unzip.exe
  へ渡します。MultiSelectModel=Player は、Shellが対応する範囲で複数選択を
  1回の起動として扱うための設定です。
#>
[CmdletBinding()]
param(
    # 省略時は、このscriptsフォルダの親にあるrecursive-unzip.exeを使う。
    [Parameter(Mandatory = $false)]
    [string]$ExecutablePath = (Join-Path $PSScriptRoot "..\recursive-unzip.exe")
)

# 未定義変数をエラーにし、失敗を握りつぶさないインストール処理にする。
Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

try {
    # 相対パスを絶対パスへ解決し、登録するコマンド文字列を一意にする。
    $resolvedExecutable = (Resolve-Path -LiteralPath $ExecutablePath).Path
}
catch {
    throw "recursive-unzip.exe が見つかりません: $ExecutablePath"
}

# 誤ってPowerShellスクリプトや別形式のファイルを登録しないよう、拡張子を確認する。
if (-not $resolvedExecutable.EndsWith(".exe", [System.StringComparison]::OrdinalIgnoreCase)) {
    throw "Windows向けの .exe ファイルを指定してください: $resolvedExecutable"
}

# SystemFileAssociationsを使い、既定のZIPアプリの関連付けを置き換えずに独自verbを追加する。
$verbKey = "HKCU:\Software\Classes\SystemFileAssociations\.zip\shell\RecursiveUnzip"
$commandKey = Join-Path $verbKey "command"

# verb本体を作り、表示名・アイコン・複数選択モデル・メニュー位置を設定する。
New-Item -Path $verbKey -Force | Out-Null
Set-ItemProperty -Path $verbKey -Name "MUIVerb" -Value "ZIPを再帰展開"
Set-ItemProperty -Path $verbKey -Name "Icon" -Value ('"{0}",0' -f $resolvedExecutable)
Set-ItemProperty -Path $verbKey -Name "MultiSelectModel" -Value "Player"
Set-ItemProperty -Path $verbKey -Name "Position" -Value "Top"

# 実行ファイルと%1を引用符で囲み、空白を含むパスが1つの引数として渡るようにする。
New-Item -Path $commandKey -Force | Out-Null
$command = '"{0}" "%1"' -f $resolvedExecutable
Set-Item -Path $commandKey -Value $command

# 登録結果を表示し、利用者が設定した実行コマンドを確認できるようにする。
Write-Host "Explorer右クリックメニューを登録しました。"
Write-Host "対象: .zip"
Write-Host "コマンド: $command"
Write-Host "Windows 11では右クリック後の『その他のオプションを表示』内に表示される場合があります。"
