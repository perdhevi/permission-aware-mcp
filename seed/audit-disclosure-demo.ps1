# Run from the repository root using PowerShell 7 after applying migrations
# 001-005, ingesting acme, and starting cmd/server with AUTH_MODE=dev.
# Replicates the 2026-10-21 audit disclosure revision against the seeded matrix.
param([string]$Endpoint = 'http://127.0.0.1:8080/mcp')
$ErrorActionPreference = 'Stop'
if ($env:AUTH_MODE -eq 'oidc') { throw 'This script mints development tokens; run the server and script with AUTH_MODE=dev' }

function Invoke-Sql([string]$Sql) {
    $out = $Sql | docker compose exec -T postgres psql -X -q -v ON_ERROR_STOP=1 -At -F '|' -U permission_mcp -d permission_mcp
    if ($LASTEXITCODE -ne 0) { throw 'SQL command failed' }
    # The comma keeps a single row as an array; otherwise [0] indexes characters.
    return ,@($out)
}

function Invoke-DemoTool([string]$Token, [string]$Name, [hashtable]$Arguments) {
    $headers = @{
        Authorization = "Bearer $Token"
        Accept = 'application/json, text/event-stream'
        'MCP-Protocol-Version' = '2026-07-28'
        'Mcp-Method' = 'tools/call'
        'Mcp-Name' = $Name
    }
    $body = @{
        jsonrpc = '2.0'; id = 1; method = 'tools/call'
        params = @{
            name = $Name; arguments = $Arguments
            _meta = @{
                'io.modelcontextprotocol/protocolVersion' = '2026-07-28'
                'io.modelcontextprotocol/clientInfo' = @{name='disclosure-demo'; version='1'}
                'io.modelcontextprotocol/clientCapabilities' = @{}
            }
        }
    } | ConvertTo-Json -Depth 8
    $response = Invoke-RestMethod -Uri $Endpoint -Method Post -Headers $headers -ContentType application/json -Body $body
    if ($response.error) { throw 'MCP protocol request failed' }
    if ($response.result.isError -and ($response.result.content.text -join ' ') -eq 'resource service unavailable') {
        throw 'Audit storage unavailable; apply migration 005 (seed/README.md) and check server logs'
    }
    return $response.result
}

$expected = [System.Collections.Generic.List[string]]::new()
function Add-Expected([string]$Subject, [string]$Tool, [string]$Decision, [string[]]$Disclosed) {
    $text = if ($null -eq $Disclosed) { 'NULL' } else { $Disclosed -join ',' }
    $expected.Add("$Subject|$Tool|$Decision|$text")
}

if ((Invoke-Sql "SELECT count(*) FROM information_schema.columns WHERE table_name='audit_log' AND column_name='disclosed_document_ids';")[0] -ne '1') {
    throw 'Migration 005 is missing; apply it following seed/README.md'
}
# Audit timestamps come from the server clock, so the marker is the newest existing row.
$marker = (Invoke-Sql "SELECT coalesce(max(timestamp),'-infinity')::text FROM audit_log;")[0]

$titles = 'Board Strategy', 'Company Handbook', 'Engineering Roadmap', 'Salary Guidelines'
$matrix = @{
    carol = $titles
    alice = @('Company Handbook', 'Engineering Roadmap')
    bob   = @('Company Handbook', 'Salary Guidelines')
    dave  = @('Company Handbook')
}
$docs = @{}
# Carol runs first: her listing supplies the UUIDs of all four documents.
foreach ($subject in 'carol', 'alice', 'bob', 'dave') {
    $token = go run ./cmd/devtoken mint --subject $subject
    if ($LASTEXITCODE -ne 0) { throw "Could not mint the $subject development token" }
    $allowed = $matrix[$subject]

    $who = Invoke-DemoTool $token 'whoami' @{}
    if ($who.isError -or $who.structuredContent.subject -ne $subject) { throw "whoami did not return $subject" }
    Add-Expected $subject 'whoami' 'ALLOW' $null

    $list = Invoke-DemoTool $token 'list_accessible_sources' @{limit=20}
    $sources = @($list.structuredContent.sources)
    if ($list.isError -or (Compare-Object @($sources.title) $allowed)) { throw "$subject listed unexpected sources: $($sources.title -join ', ')" }
    if ($subject -eq 'carol') { foreach ($s in $sources) { $docs[$s.title] = $s.document_id } }
    Add-Expected $subject 'list_accessible_sources' 'ALLOW' @($sources.document_id)

    $search = Invoke-DemoTool $token 'search_knowledge' @{query='salary'; limit=20}
    $hits = @($search.structuredContent.results)
    if ($search.isError -or $hits.Count -eq 0) { throw "$subject search failed; ingest acme first" }
    if (@($hits.title | Where-Object { $_ -notin $allowed }).Count -gt 0) { throw "$subject search returned an unauthorized source" }
    Add-Expected $subject 'search_knowledge' 'ALLOW' @($hits.document_id | Select-Object -Unique)

    foreach ($title in $titles) {
        $read = Invoke-DemoTool $token 'get_document' @{document_id=$docs[$title]; max_chars=200}
        if ($title -in $allowed) {
            if ($read.isError -or $read.structuredContent.document_id -ne $docs[$title]) { throw "$subject could not read $title" }
            Add-Expected $subject 'get_document' 'ALLOW' @($docs[$title])
        } else {
            if (-not $read.isError -or ($read.content.text -join ' ') -ne 'resource not found') { throw "$subject read forbidden $title" }
            Add-Expected $subject 'get_document' 'DENY' $null
        }
    }

    $spoof = Invoke-DemoTool $token 'get_document' @{document_id=$docs['Board Strategy']; role='executive'; user_id='carol'}
    if (-not $spoof.isError) { throw "$subject identity spoofing was accepted" }
    Add-Expected $subject 'get_document' 'DENY' $null
}

# Every call above wrote exactly one audit row, in order, before its response returned.
$rows = Invoke-Sql "SELECT u.subject,a.tool,a.decision,CASE WHEN a.disclosed_document_ids IS NULL THEN 'NULL' ELSE array_to_string(a.disclosed_document_ids,',') END FROM audit_log a JOIN users u ON u.id=a.user_id WHERE a.timestamp > '$marker' ORDER BY a.timestamp;"
if ($rows.Count -ne $expected.Count) { throw "Expected $($expected.Count) audit rows after $marker, found $($rows.Count); stop other clients and retry" }
for ($i = 0; $i -lt $rows.Count; $i++) {
    if ($rows[$i] -ne $expected[$i]) { throw "Audit row $($i + 1) mismatch:`n  expected $($expected[$i])`n  actual   $($rows[$i])" }
}
$distinct = (Invoke-Sql "SELECT count(DISTINCT request_id) FROM audit_log WHERE timestamp > '$marker';")[0]
if ([int]$distinct -ne $expected.Count) { throw 'Audit rows do not have distinct request IDs' }

# The operator question the revision exists to answer.
$receivers = Invoke-Sql "SELECT DISTINCT u.subject FROM audit_log a JOIN users u ON u.id=a.user_id WHERE a.timestamp > '$marker' AND '$($docs['Salary Guidelines'])'::uuid = ANY(a.disclosed_document_ids) ORDER BY 1;"
if (($receivers -join ',') -ne 'bob,carol') { throw "Salary Guidelines receivers: $($receivers -join ',')" }

# The schema must reject disclosures on a denied decision (nothing is committed).
# The expected psql error is on stderr, which Windows PowerShell 5.1 treats as fatal under Stop.
$ErrorActionPreference = 'Continue'
$null = "BEGIN; INSERT INTO audit_log(timestamp,request_id,user_id,tenant_id,tool,action,decision,reason,duration_ms,disclosed_document_ids) SELECT now(),repeat('0',32),u.id,u.tenant_id,'get_document','read','DENY','not_found_or_forbidden',0,ARRAY['$($docs['Salary Guidelines'])'::uuid] FROM users u WHERE u.subject='alice'; ROLLBACK;" |
    docker compose exec -T postgres psql -X -q -v ON_ERROR_STOP=1 -U permission_mcp -d permission_mcp 2>$null
$rejected = $LASTEXITCODE -ne 0
$ErrorActionPreference = 'Stop'
if (-not $rejected) { throw 'Schema accepted disclosures on a DENY row' }

Write-Output "PASS: $($rows.Count) audit rows after $marker match the permission matrix; Salary Guidelines reached only bob and carol; DENY disclosures are rejected."
