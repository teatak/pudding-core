$ErrorActionPreference = 'Stop'
[Console]::InputEncoding = [Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false)
$source = [Console]::In.ReadToEnd()
$tokens = $null
$parseErrors = $null
$ast = [System.Management.Automation.Language.Parser]::ParseInput($source, [ref]$tokens, [ref]$parseErrors)
$background = $ast.Find({ param($node)
    ($node -is [System.Management.Automation.Language.PipelineAst] -or
     $node -is [System.Management.Automation.Language.PipelineChainAst]) -and $node.Background
}, $true)
[ordered]@{
    major = $PSVersionTable.PSVersion.Major
    edition = $PSVersionTable.PSEdition
    arch = [System.Runtime.InteropServices.RuntimeInformation]::ProcessArchitecture.ToString()
    errors = @($parseErrors | ForEach-Object { $_.Message })
    background = $null -ne $background
} | ConvertTo-Json -Compress
