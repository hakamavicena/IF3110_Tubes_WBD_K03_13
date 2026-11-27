# scripts/smoke.ps1 - Automated End-to-End Smoke Test for Milestone 1 & Milestone 2 JSON REST API
# Menguji 10 skenario kunci terhadap server Go REST API yang sedang berjalan

param (
    [string]$BaseUrl = "http://localhost:8080",
    [int]$DbTargetId = 1
)

$ErrorActionPreference = "Continue"
$failedCount = 0

function Report-Result {
    param ([string]$Name, [bool]$Passed, [string]$Detail)
    if ($Passed) {
        Write-Host "PASS: $Name" -ForegroundColor Green
    } else {
        Write-Host "FAIL: $Name - $Detail" -ForegroundColor Red
        $script:failedCount++
    }
}

function Get-CsrfToken {
    param ([string]$CookieFile)
    $json = & curl.exe -s -b "$CookieFile" "$BaseUrl/api/auth/me"
    try { return ($json | ConvertFrom-Json).csrf_token } catch { return "" }
}

function Login-User {
    param ([string]$Username, [string]$Password, [string]$CookieFile)
    if (Test-Path $CookieFile) { Remove-Item $CookieFile -Force }
    $payload = '{\"username\":\"' + $Username + '\",\"password\":\"' + $Password + '\"}'
    $res = & curl.exe -s -c "$CookieFile" -H "Content-Type: application/json" -H "X-Requested-With: XMLHttpRequest" -X POST "$BaseUrl/api/auth/login" -d $payload -w "%{http_code}" -o /dev/null
    return $res
}

Write-Host "=== Memulai Smoke Test JSON REST API (Milestone 1 & 2) di $BaseUrl ===" -ForegroundColor Cyan

# ----------------------------------------------------
# 1. Tanpa cookie -> 401 Unauthorized
# ----------------------------------------------------
$code1 = & curl.exe -s -o /dev/null -w "%{http_code}" "$BaseUrl/api/databases"
Report-Result -Name "1. Akses tanpa cookie -> 401" -Passed ($code1 -eq "401") -Detail "Got HTTP $code1"

# ----------------------------------------------------
# 2. Outsider -> 403 pada /api/databases/{id}/schemas
# ----------------------------------------------------
$cookieOutsider = "$env:TEMP\cookie_outsider.txt"
Login-User "outsider" "password123" $cookieOutsider | Out-Null
$code2 = & curl.exe -s -b $cookieOutsider -o /dev/null -w "%{http_code}" "$BaseUrl/api/databases/$DbTargetId/schemas"
Report-Result -Name "2. Outsider -> 403 pada /api/databases/$DbTargetId/schemas" -Passed ($code2 -eq "403") -Detail "Got HTTP $code2"

# ----------------------------------------------------
# 3. Admin tanpa group -> 403 pada database target
# ----------------------------------------------------
$cookieAdmin = "$env:TEMP\cookie_admin.txt"
$adminLoginCode = Login-User "admin" "password123" $cookieAdmin
if ($adminLoginCode -ne "200") {
    Login-User "admin" "rahasia123" $cookieAdmin | Out-Null
}
$code3 = & curl.exe -s -b $cookieAdmin -o /dev/null -w "%{http_code}" "$BaseUrl/api/databases/$DbTargetId/schemas"
Report-Result -Name "3. Admin tanpa group -> 403 pada /api/databases/$DbTargetId/schemas" -Passed ($code3 -eq "403") -Detail "Got HTTP $code3"

# ----------------------------------------------------
# 4. Viewer: GET 200, POST create migration dengan token valid -> 403
# ----------------------------------------------------
$cookieViewer = "$env:TEMP\cookie_viewer.txt"
Login-User "viewer" "password123" $cookieViewer | Out-Null
$code4Get = & curl.exe -s -b $cookieViewer -o /dev/null -w "%{http_code}" "$BaseUrl/api/databases/$DbTargetId/schemas"
$csrfViewer = Get-CsrfToken $cookieViewer

$postViewer = '{\"title\":\"ForbiddenMig\",\"sql\":\"SELECT 1;\",\"sql_down\":\"SELECT 1;\",\"is_irreversible\":false,\"execution_mode\":\"standard\"}'
$code4Post = & curl.exe -s -b $cookieViewer -o /dev/null -w "%{http_code}" -H "Content-Type: application/json" -H "X-CSRF-Token: $csrfViewer" -X POST "$BaseUrl/api/databases/$DbTargetId/migrations" -d $postViewer
$pass4 = ($code4Get -eq "200") -and ($code4Post -eq "403")
Report-Result -Name "4. Viewer: GET 200, POST create migration -> 403" -Passed $pass4 -Detail "GET: $code4Get, POST: $code4Post"

# ----------------------------------------------------
# 5. Editor: Create Migration M2, Enqueue sebelum Approved -> 409 Conflict
# ----------------------------------------------------
$cookieEditor = "$env:TEMP\cookie_editor.txt"
Login-User "editor" "password123" $cookieEditor | Out-Null
$csrfEditor = Get-CsrfToken $cookieEditor

$migNameA = "smoke_tbl_a_" + (Get-Random -Minimum 1000 -Maximum 9999)
$payloadA = '{\"title\":\"SmokeA\",\"sql\":\"CREATE TABLE ' + $migNameA + ' (id serial primary key);\",\"sql_down\":\"DROP TABLE IF EXISTS ' + $migNameA + ';\",\"is_irreversible\":false,\"execution_mode\":\"standard\"}'
$createAJson = & curl.exe -s -b "$cookieEditor" -H "Content-Type: application/json" -H "X-CSRF-Token: $csrfEditor" -X POST "$BaseUrl/api/databases/$DbTargetId/migrations" -d $payloadA
$migIdA = 0
$jobIdA = 0
try {
    $parsedA = $createAJson | ConvertFrom-Json
    $migIdA = [int]$parsedA.migration.id
    $jobIdA = [int]$parsedA.job.id
} catch {}

# Coba enqueue job yang masih draft (belum approved oleh leader) -> HARUS 409 Conflict
$codeEnqueueUnapproved = & curl.exe -s -b "$cookieEditor" -o /dev/null -w "%{http_code}" -H "X-CSRF-Token: $csrfEditor" -X POST "$BaseUrl/api/databases/$DbTargetId/jobs/$jobIdA/enqueue"

$pass5 = ($migIdA -gt 0) -and ($jobIdA -gt 0) -and ($codeEnqueueUnapproved -eq "409")
Report-Result -Name "5. Editor: buat migration M2 (201) & tolak enqueue sebelum approved (409)" -Passed $pass5 -Detail "MigID: $migIdA, JobID: $jobIdA, EnqueueUnapproved: $codeEnqueueUnapproved"

# ----------------------------------------------------
# 6. Multi bisa akses migrations (punya 2 grup, level tertinggi menang)
# ----------------------------------------------------
$cookieMulti = "$env:TEMP\cookie_multi.txt"
Login-User "multi" "password123" $cookieMulti | Out-Null
$code6 = & curl.exe -s -b "$cookieMulti" -o /dev/null -w "%{http_code}" "$BaseUrl/api/databases/$DbTargetId/migrations"
Report-Result -Name "6. Multi bisa akses migrations (akses level 2)" -Passed ($code6 -eq "200") -Detail "Got HTTP $code6"

# ----------------------------------------------------
# 7. IDOR: akses resource migrasi yang tidak ada -> 404
# ----------------------------------------------------
$code7 = & curl.exe -s -b "$cookieEditor" -o /dev/null -w "%{http_code}" "$BaseUrl/api/databases/$DbTargetId/migrations/999999"
Report-Result -Name "7. IDOR -> 404" -Passed ($code7 -eq "404") -Detail "Got HTTP $code7"

# ----------------------------------------------------
# 8. POST tanpa CSRF -> 403 Forbidden
# ----------------------------------------------------
$code8 = & curl.exe -s -b "$cookieEditor" -o /dev/null -w "%{http_code}" -H "Content-Type: application/json" -X POST "$BaseUrl/api/databases/$DbTargetId/migrations" -d $postViewer
Report-Result -Name "8. POST tanpa CSRF -> 403" -Passed ($code8 -eq "403") -Detail "Got HTTP $code8"

# ----------------------------------------------------
# 9. Logout lalu replay cookie lama -> 401
# ----------------------------------------------------
$cookieLogout = "$env:TEMP\cookie_logout.txt"
Login-User "editor" "password123" $cookieLogout | Out-Null
$csrfLogout = Get-CsrfToken $cookieLogout

& curl.exe -s -b "$cookieLogout" -c "$cookieLogout" -o /dev/null -w "%{http_code}" -X POST "$BaseUrl/api/auth/logout" -H "X-CSRF-Token: $csrfLogout" | Out-Null
$code9Replay = & curl.exe -s -b "$cookieLogout" -o /dev/null -w "%{http_code}" "$BaseUrl/api/databases"
$pass9 = ($code9Replay -eq "401")
Report-Result -Name "9. Logout lalu replay cookie lama -> 401" -Passed $pass9 -Detail "Replay returned HTTP $code9Replay"

# ----------------------------------------------------
# 10. Admin menonaktifkan user lalu session user itu langsung 401
# ----------------------------------------------------
$cookieTemp = "$env:TEMP\cookie_temp_user.txt"
Login-User "outsider" "password123" $cookieTemp | Out-Null
$csrfAdmin = Get-CsrfToken $cookieAdmin

$usersJson = & curl.exe -s -b $cookieAdmin "$BaseUrl/api/admin/users"
$outsiderId = 4
try { $outsiderId = [int](($usersJson | ConvertFrom-Json) | Where-Object { $_.username -eq "outsider" } | Select-Object -First 1).id } catch {}

& curl.exe -s -b $cookieAdmin -o /dev/null -X PATCH "$BaseUrl/api/admin/users/$outsiderId/status" -H "Content-Type: application/json" -H "X-CSRF-Token: $csrfAdmin" -d '{\"is_active\":false}'
$code10Deact = & curl.exe -s -b $cookieTemp -o /dev/null -w "%{http_code}" "$BaseUrl/api/databases"
& curl.exe -s -b $cookieAdmin -o /dev/null -X PATCH "$BaseUrl/api/admin/users/$outsiderId/status" -H "Content-Type: application/json" -H "X-CSRF-Token: $csrfAdmin" -d '{\"is_active\":true}'

$pass10 = ($code10Deact -eq "401")
Report-Result -Name "10. Admin menonaktifkan user -> session langsung 401" -Passed $pass10 -Detail "Session check returned HTTP $code10Deact"

Write-Host ""
if ($script:failedCount -eq 0) {
    Write-Host "SELURUH SMOKE TEST BERHASIL (10/10 PASS)!" -ForegroundColor Green
    exit 0
} else {
    Write-Host "TERDAPAT $script:failedCount TEST GAGAL!" -ForegroundColor Red
    exit 1
}
