$ErrorActionPreference = 'Stop'

$packageName = 'cojira'
$toolsDir = Split-Path -Parent $MyInvocation.MyCommand.Definition
$zipUrl = '__WINDOWS_AMD64_ZIP_URL__'
$checksum = '__WINDOWS_AMD64_SHA256__'

Install-ChocolateyZipPackage `
  -PackageName $packageName `
  -Url $zipUrl `
  -UnzipLocation $toolsDir `
  -Checksum $checksum `
  -ChecksumType 'sha256'

Install-BinFile -Name 'cojira' -Path (Join-Path $toolsDir 'cojira.exe')
