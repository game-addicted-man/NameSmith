# Turns the end of a failed step's log into a GitHub annotation, so the
# failure can be read from the run's summary (and through the API).
param([string]$Title, [string]$LogFile, [int]$Lines = 80)
$text = "(no log)"
if (Test-Path $LogFile) { $text = (Get-Content $LogFile -Tail $Lines) -join "`n" }
$text = $text.Replace('%', '%25').Replace("`r", '%0D').Replace("`n", '%0A')
Write-Output "::error title=$Title::$text"
