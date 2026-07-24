# Test harness: appends chat-log lines using a file handle opened with
# FileShare.ReadWrite (exactly like the real Project Gorgon client does),
# so the tailer's concurrent read handle doesn't block our writes.
param(
  [Parameter(Mandatory=$true)][string]$Path,
  [Parameter(Mandatory=$true)][string[]]$Lines
)
foreach ($line in $Lines) {
  $fs = [System.IO.File]::Open(
    $Path,
    [System.IO.FileMode]::Append,
    [System.IO.FileAccess]::Write,
    [System.IO.FileShare]::ReadWrite
  )
  $sw = New-Object System.IO.StreamWriter($fs, (New-Object System.Text.UTF8Encoding($false)))
  $sw.WriteLine($line)
  $sw.Flush()
  $sw.Close()
  $fs.Close()
  Start-Sleep -Milliseconds 250
}