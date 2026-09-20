$scriptPath = Join-Path $PSScriptRoot "Install-ExplorerMenu.ps1"

Describe "Install-ExplorerMenu" {
    It "registers one Player command that forwards every selected ZIP" {
        $executablePath = Join-Path $TestDrive "recursive-unzip.exe"
        New-Item -ItemType File -Path $executablePath | Out-Null

        Mock New-Item { }
        Mock Set-ItemProperty { }
        Mock Set-Item { }
        Mock Write-Host { }

        & $scriptPath -ExecutablePath $executablePath

        $verbKey = "HKCU:\Software\Classes\SystemFileAssociations\.zip\shell\RecursiveUnzip"
        $commandKey = Join-Path $verbKey "command"
        $expectedCommand = ('"{0}" --delete-nested-zip --no-open %*' -f $executablePath)

        Assert-MockCalled Set-ItemProperty -Scope It -ParameterFilter {
            $Path -eq $verbKey -and $Name -eq "MultiSelectModel" -and $Value -eq "Player"
        }
        Assert-MockCalled Set-Item -Scope It -ParameterFilter {
            $Path -eq $commandKey -and $Value -eq $expectedCommand
        }
    }
}
