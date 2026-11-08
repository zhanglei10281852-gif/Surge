param([string]$ReportPath)
$ErrorActionPreference='Stop'
try {
Add-Type @'
using System;
using System.Runtime.InteropServices;
public static class SurgeReviewConsole {
 [StructLayout(LayoutKind.Sequential)] public struct COORD {public short X; public short Y;}
 [StructLayout(LayoutKind.Sequential,CharSet=CharSet.Unicode)] public struct FONTINFO {
 public uint cbSize; public uint nFont; public COORD dwFontSize; public int FontFamily; public int FontWeight;
 [MarshalAs(UnmanagedType.ByValTStr,SizeConst=32)] public string FaceName; }
 [DllImport("kernel32.dll",CharSet=CharSet.Unicode,SetLastError=true)] public static extern IntPtr CreateFile(string n,uint access,uint share,IntPtr security,uint creation,uint flags,IntPtr template);
 [DllImport("kernel32.dll")] public static extern IntPtr GetStdHandle(int n);
 [DllImport("kernel32.dll")] public static extern bool GetCurrentConsoleFontEx(IntPtr h,bool max,ref FONTINFO f);
 [DllImport("kernel32.dll",SetLastError=true)] public static extern bool SetCurrentConsoleFontEx(IntPtr h,bool max,ref FONTINFO f);
}
'@
$consoleHandle=[SurgeReviewConsole]::CreateFile('CONOUT$',3221225472,3,[IntPtr]::Zero,3,0,[IntPtr]::Zero)
$f=New-Object SurgeReviewConsole+FONTINFO
$f.cbSize=[Runtime.InteropServices.Marshal]::SizeOf($f)
$coord=New-Object SurgeReviewConsole+COORD
$coord.X=10; $coord.Y=20
$f.dwFontSize=$coord
$f.FontFamily=54; $f.FontWeight=400; $f.FaceName='Consolas'
$ok=[SurgeReviewConsole]::SetCurrentConsoleFontEx($consoleHandle,$false,[ref]$f)
"handle=$consoleHandle ok=$ok error=$([Runtime.InteropServices.Marshal]::GetLastWin32Error()) size=$($f.cbSize)" | Add-Content F:\swe\work\surge-delivery-review\font-diagnostic.txt
[void][SurgeReviewConsole]::GetCurrentConsoleFontEx($consoleHandle,$false,[ref]$f)
$f | Format-List * | Out-File F:\swe\work\surge-delivery-review\font-current.txt
$raw=$Host.UI.RawUI
$raw.WindowSize=New-Object Management.Automation.Host.Size(100,30)
$raw.BufferSize=New-Object Management.Automation.Host.Size(240,150)
$raw.WindowSize=New-Object Management.Automation.Host.Size(240,64)
$raw.WindowTitle='SWE-REVIEW-surge-8ba94984'
$raw.BackgroundColor='Black';$raw.ForegroundColor='White'
Start-Sleep -Seconds 4
$ok=[SurgeReviewConsole]::SetCurrentConsoleFontEx($consoleHandle,$false,[ref]$f)
"handle=$consoleHandle ok=$ok error=$([Runtime.InteropServices.Marshal]::GetLastWin32Error()) size=$($f.cbSize)" | Add-Content F:\swe\work\surge-delivery-review\font-diagnostic.txt
[void][SurgeReviewConsole]::GetCurrentConsoleFontEx($consoleHandle,$false,[ref]$f)
$f | Format-List * | Out-File F:\swe\work\surge-delivery-review\font-current.txt
$raw.WindowSize=New-Object Management.Automation.Host.Size(100,30)
$raw.BufferSize=New-Object Management.Automation.Host.Size(240,150)
$raw.WindowSize=New-Object Management.Automation.Host.Size(240,64)
$raw | Format-List * | Out-File F:\swe\work\surge-delivery-review\console-diagnostic.txt
Clear-Host
Get-Content -LiteralPath $ReportPath -Encoding UTF8 | ForEach-Object { if ($_ -match '^Rubric ID：6|^bad.*-once:|^断言|^代码审计闭环|^internal/') { Write-Host $_ -ForegroundColor Yellow } else { Write-Host $_ } }
$raw.WindowPosition=New-Object Management.Automation.Host.Coordinates(0,0)
while($true){Start-Sleep -Seconds 60}

} catch { $_ | Out-String | Set-Content F:\swe\work\surge-delivery-review\console-error.txt; while($true){Start-Sleep -Seconds 60} }



