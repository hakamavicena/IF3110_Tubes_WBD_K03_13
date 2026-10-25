# scripts/smoke.ps1 - Automated End-to-End Smoke Test for Milestone 1
# Menguji 10 skenario kunci terhadap server yang sedang berjalan

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

function Extract-CsrfToken {
    param ([string]$Html)
    if ($Html -match 'name="csrf_token"\s+value="([^"]+)"') {
        return $Matches[1]
    }
    if ($Html -match 'value="([^"]+)"\s+name="csrf_token"') {
        return $Matches[1]
    }
    return ""
}

function Login-User {
    param ([string]$Username, [string]$Password, [string]$CookieFile)
    if (Test-Path $CookieFile) { Remove-Item $CookieFile -Force }
    $payload = '{\"username\":\"' + $Username + '\",\"password\":\"' + $Password + '\"}'
    $res = & curl.exe -s -c "$CookieFile" -H "Content-Type: application/json" -H "X-Requested-With: XMLHttpRequest" -X POST "$BaseUrl/login" -d $payload -w "%{http_code}" -o /dev/null
    return $res
}

Write-Host "=== Memulai Smoke Test Milestone 1 di $BaseUrl ===" -ForegroundColor Cyan

# ----------------------------------------------------
# 1. Tanpa cookie -> 401 Unauthorized
# ----------------------------------------------------
$code1 = & curl.exe -s -o /dev/null -w "%{http_code}" "$BaseUrl/dashboard"
Report-Result -Name "1. Akses tanpa cookie -> 401" -Passed ($code1 -eq "401") -Detail "Got HTTP $code1"

# ----------------------------------------------------
# 2. Outsider -> 403 pada /databases/{id}
# ----------------------------------------------------
$cookieOutsider = "$env:TEMP\cookie_outsider.txt"
Login-User "outsider" "password123" $cookieOutsider | Out-Null
$code2 = & curl.exe -s -b $cookieOutsider -o /dev/null -w "%{http_code}" "$BaseUrl/databases/$DbTargetId"
Report-Result -Name "2. Outsider -> 403 pada /databases/$DbTargetId" -Passed ($code2 -eq "403") -Detail "Got HTTP $code2"

# ----------------------------------------------------
# 3. Admin tanpa group -> 403 pada database target
# ----------------------------------------------------
$cookieAdmin = "$env:TEMP\cookie_admin.txt"
$adminLoginCode = Login-User "admin" "password123" $cookieAdmin
if ($adminLoginCode -ne "200") {
    Login-User "admin" "rahasia123" $cookieAdmin | Out-Null
}
$code3 = & curl.exe -s -b $cookieAdmin -o /dev/null -w "%{http_code}" "$BaseUrl/databases/$DbTargetId"
Report-Result -Name "3. Admin tanpa group -> 403 pada /databases/$DbTargetId" -Passed ($code3 -eq "403") -Detail "Got HTTP $code3"

# ----------------------------------------------------
# 4. Viewer: GET 200, POST create dengan token valid -> 403
# ----------------------------------------------------
$cookieViewer = "$env:TEMP\cookie_viewer.txt"
Login-User "viewer" "password123" $cookieViewer | Out-Null
$code4Get = & curl.exe -s -b $cookieViewer -o /dev/null -w "%{http_code}" "$BaseUrl/databases/$DbTargetId"
$dashHtmlViewer = & curl.exe -s -b $cookieViewer "$BaseUrl/dashboard"
$csrfViewer = Extract-CsrfToken $dashHtmlViewer

$postViewer = "csrf_token=$csrfViewer&title=ForbiddenMig&sql=SELECT+1;"
$code4Post = & curl.exe -s -b $cookieViewer -o /dev/null -w "%{http_code}" -X POST "$BaseUrl/databases/$DbTargetId/migrations" -d $postViewer
$pass4 = ($code4Get -eq "200") -and ($code4Post -eq "403")
Report-Result -Name "4. Viewer: GET 200, POST create migration -> 403" -Passed $pass4 -Detail "GET: $code4Get, POST: $code4Post"

# ----------------------------------------------------
# 5. Editor: migration workflow, atomisitas, loncat versi -> 409
# ----------------------------------------------------
$cookieEditor = "$env:TEMP\cookie_editor.txt"
Login-User "editor" "password123" $cookieEditor | Out-Null
$dashHtmlEditor = & curl.exe -s -b $cookieEditor "$BaseUrl/dashboard"
$csrfEditor = Extract-CsrfToken $dashHtmlEditor

# 5a. Buat migrasi valid A
$migNameA = "smoke_tbl_a_" + (Get-Random -Minimum 1000 -Maximum 9999)
$sqlA = "CREATE TABLE $migNameA (id serial primary key);"
$createA = & curl.exe -s -i -b "$cookieEditor" -X POST "$BaseUrl/databases/$DbTargetId/migrations" --data-urlencode "csrf_token=$csrfEditor" --data-urlencode "title=SmokeA" --data-urlencode "sql=$sqlA"
$createAStr = $createA -join "`n"
$migIdA = ""
if ($createAStr -match 'Location:.*\/migrations\/(\d+)') {
    $migIdA = $Matches[1]
}

# 5b. Buat migrasi B (versi loncat / berikutnya)
$migNameB = "smoke_tbl_b_" + (Get-Random -Minimum 1000 -Maximum 9999)
$sqlB = "CREATE TABLE $migNameB (id serial primary key);"
$createB = & curl.exe -s -i -b "$cookieEditor" -X POST "$BaseUrl/databases/$DbTargetId/migrations" --data-urlencode "csrf_token=$csrfEditor" --data-urlencode "title=SmokeB" --data-urlencode "sql=$sqlB"
$createBStr = $createB -join "`n"
$migIdB = ""
if ($createBStr -match 'Location:.*\/migrations\/(\d+)') {
    $migIdB = $Matches[1]
}

# Coba eksekusi B lebih dulu (versi loncat) -> HARUS 409 Conflict
$codeGap = & curl.exe -s -b "$cookieEditor" -o /dev/null -w "%{http_code}" -X POST "$BaseUrl/databases/$DbTargetId/migrations/$migIdB/execute" --data-urlencode "csrf_token=$csrfEditor"

# Eksekusi A secara berurutan -> Sukses
$codeExecA = & curl.exe -s -b "$cookieEditor" -o /dev/null -w "%{http_code}" -X POST "$BaseUrl/databases/$DbTargetId/migrations/$migIdA/execute" --data-urlencode "csrf_token=$csrfEditor"

# Eksekusi B setelah A sukses -> Sukses
$codeExecB = & curl.exe -s -b "$cookieEditor" -o /dev/null -w "%{http_code}" -X POST "$BaseUrl/databases/$DbTargetId/migrations/$migIdB/execute" --data-urlencode "csrf_token=$csrfEditor"

# 5c. Atomisitas transaksi migrasi yang gagal (statement kedua gagal)
$atomicTable = "smoke_atomic_" + (Get-Random -Minimum 1000 -Maximum 9999)
$sqlAtomic = "CREATE TABLE $atomicTable (id int); MALFORMED_SYNTAX_ERROR;"
$createC = & curl.exe -s -i -b "$cookieEditor" -X POST "$BaseUrl/databases/$DbTargetId/migrations" --data-urlencode "csrf_token=$csrfEditor" --data-urlencode "title=AtomicFail" --data-urlencode "sql=$sqlAtomic"
$createCStr = $createC -join "`n"
$migIdC = ""
if ($createCStr -match 'Location:.*\/migrations\/(\d+)') {
    $migIdC = $Matches[1]
}
$codeExecC = & curl.exe -s -b "$cookieEditor" -o /dev/null -w "%{http_code}" -X POST "$BaseUrl/databases/$DbTargetId/migrations/$migIdC/execute" --data-urlencode "csrf_token=$csrfEditor"

# Periksa tabel tidak boleh tercipta di database target (atomisitas rollback)
$checkTableHtml = & curl.exe -s -b "$cookieEditor" "$BaseUrl/databases/$DbTargetId/schemas/public/tables"
$atomicTableExists = $checkTableHtml.Contains($atomicTable)

$pass5 = ($codeGap -eq "409") -and ($codeExecA -in @("200", "303")) -and ($codeExecB -in @("200", "303")) -and (-not $atomicTableExists)
Report-Result -Name "5. Editor: migrasi linear, loncat versi -> 409, atomisitas rollback saat gagal" -Passed $pass5 -Detail "GapCode: $codeGap, ExecA: $codeExecA, ExecB: $codeExecB, TableExists: $atomicTableExists"

# ----------------------------------------------------
# 6. Multi bisa edit (punya 2 grup, level tertinggi menang)
# ----------------------------------------------------
$cookieMulti = "$env:TEMP\cookie_multi.txt"
Login-User "multi" "password123" $cookieMulti | Out-Null
$code6 = & curl.exe -s -b "$cookieMulti" -o /dev/null -w "%{http_code}" "$BaseUrl/databases/$DbTargetId/migrations/new"
Report-Result -Name "6. Multi bisa edit (akses level 2)" -Passed ($code6 -eq "200") -Detail "Got HTTP $code6"

# ----------------------------------------------------
# 7. IDOR: akses resource migrasi yang tidak ada -> 404
# ----------------------------------------------------
$code7 = & curl.exe -s -b "$cookieEditor" -o /dev/null -w "%{http_code}" "$BaseUrl/databases/$DbTargetId/migrations/999999"
Report-Result -Name "7. IDOR -> 404" -Passed ($code7 -eq "404") -Detail "Got HTTP $code7"

# ----------------------------------------------------
# 8. POST tanpa CSRF -> 403 Forbidden
# ----------------------------------------------------
$code8 = & curl.exe -s -b "$cookieEditor" -o /dev/null -w "%{http_code}" -X POST "$BaseUrl/databases/$DbTargetId/migrations" -d "title=NoCSRF&sql=SELECT+1;"
Report-Result -Name "8. POST tanpa CSRF -> 403" -Passed ($code8 -eq "403") -Detail "Got HTTP $code8"

# ----------------------------------------------------
# 9. Logout lalu replay cookie lama -> 401
# ----------------------------------------------------
$cookieLogout = "$env:TEMP\cookie_logout.txt"
Login-User "editor" "password123" $cookieLogout | Out-Null
$dashHtmlLogout = & curl.exe -s -b "$cookieLogout" "$BaseUrl/dashboard"
$csrfLogout = Extract-CsrfToken $dashHtmlLogout

$logoutPost = "csrf_token=$csrfLogout"
$code9Logout = & curl.exe -s -b "$cookieLogout" -c "$cookieLogout" -o /dev/null -w "%{http_code}" -X POST "$BaseUrl/logout" -d $logoutPost
$code9Replay = & curl.exe -s -b "$cookieLogout" -o /dev/null -w "%{http_code}" "$BaseUrl/dashboard"
$pass9 = ($code9Replay -eq "401")
Report-Result -Name "9. Logout lalu replay cookie lama -> 401" -Passed $pass9 -Detail "Replay returned HTTP $code9Replay"

# ----------------------------------------------------
# 10. Admin menonaktifkan user lalu session user itu langsung 401
# ----------------------------------------------------
$cookieTemp = "$env:TEMP\cookie_temp_user.txt"
Login-User "outsider" "password123" $cookieTemp | Out-Null
$dashHtmlAdmin = & curl.exe -s -b $cookieAdmin "$BaseUrl/admin"
$csrfAdmin = Extract-CsrfToken $dashHtmlAdmin

# Cari User ID outsider dari HTML admin
$outsiderId = 4
if ($dashHtmlAdmin -match '<td><strong>outsider</strong></td>[\s\S]*?<input type="hidden" name="user_id" value="(\d+)">') {
    $outsiderId = [int]$Matches[1]
}

# Admin nonaktifkan outsider
$postDeact = "csrf_token=$csrfAdmin&user_id=$outsiderId&action=deactivate"
& curl.exe -s -b $cookieAdmin -o /dev/null -X POST "$BaseUrl/admin/users/status" -d $postDeact

# Sesi aktif outsider harus langsung ditolak 401
$code10Deact = & curl.exe -s -b $cookieTemp -o /dev/null -w "%{http_code}" "$BaseUrl/dashboard"

# Aktifkan kembali agar skrip dapat diulang
$postAct = "csrf_token=$csrfAdmin&user_id=$outsiderId&action=activate"
& curl.exe -s -b $cookieAdmin -o /dev/null -X POST "$BaseUrl/admin/users/status" -d $postAct

$pass10 = ($code10Deact -eq "401")
Report-Result -Name "10. Admin menonaktifkan user -> session langsung 401" -Passed $pass10 -Detail "Session check returned HTTP $code10Deact"

# ----------------------------------------------------
# Ringkasan Akhir
# ----------------------------------------------------
Write-Host ""
if ($script:failedCount -eq 0) {
    Write-Host "SELURUH SMOKE TEST BERHASIL (10/10 PASS)!" -ForegroundColor Green
    exit 0
} else {
    Write-Host "TERDAPAT $script:failedCount TEST GAGAL!" -ForegroundColor Red
    exit 1
}
