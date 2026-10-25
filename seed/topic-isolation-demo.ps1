# Run from the repository root (Windows PowerShell 5.1 or PowerShell 7) after
# applying seed/topic-isolation.sql, re-ingesting acme, and starting cmd/server
# with AUTH_MODE=dev. Compares the same "Project Falcon" query across all four
# seeded callers, then cross-checks direct reads of every Falcon document.
param([string]$Endpoint = 'http://127.0.0.1:8080/mcp', [string]$Query = 'Project Falcon migration')
$ErrorActionPreference = 'Stop'
$callers = 'alice', 'bob', 'carol', 'dave'
# Expected Falcon documents per caller: the shared overview plus at most one exclusive document.
$expected = @{
    alice = @('Project Falcon Overview', 'Project Falcon Technical Design')
    bob   = @('Project Falcon Overview', 'Project Falcon Staffing Plan')
    carol = @('Project Falcon Budget Approval', 'Project Falcon Overview')
    dave  = @('Project Falcon Overview')
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
                'io.modelcontextprotocol/clientInfo' = @{name='topic-isolation-demo'; version='1'}
                'io.modelcontextprotocol/clientCapabilities' = @{}
            }
        }
    } | ConvertTo-Json -Depth 8
    $response = Invoke-RestMethod -Uri $Endpoint -Method Post -Headers $headers -ContentType application/json -Body $body
    if ($response.error) { throw 'MCP protocol request failed' }
    return $response.result
}

$tokens = @{}
foreach ($caller in $callers) {
    $tokens[$caller] = go run ./cmd/devtoken mint --subject $caller
    if ($LASTEXITCODE -ne 0) { throw "Could not mint the $caller development token" }
}

# 1. Same query, four callers.
$failures = @()
$falconIDs = @{}
Write-Output "Search: '$Query'"
foreach ($caller in $callers) {
    $search = Invoke-DemoTool $tokens[$caller] 'search_knowledge' @{query=$Query; limit=10}
    if ($search.isError) { throw "Search failed for $caller; check the fixture and re-ingest acme" }
    $hits = @($search.structuredContent.results | Where-Object { $_.title -like 'Project Falcon*' })
    foreach ($hit in $hits) { $falconIDs[$hit.title] = $hit.document_id }
    $titles = @($hits | ForEach-Object { $_.title } | Sort-Object -Unique)
    Write-Output ('  {0,-6} {1}' -f $caller, ($titles -join ', '))
    if (($titles -join '|') -ne (($expected[$caller] | Sort-Object) -join '|')) {
        $failures += "$caller search returned [$($titles -join ', ')]"
    }
}
if ($falconIDs.Count -ne 4) { throw "Expected 4 Falcon documents across all callers, found $($falconIDs.Count); apply seed/topic-isolation.sql and re-ingest acme" }

# 2. Knowing the ID is not access: every caller tries every Falcon document.
Write-Output ''
Write-Output 'Direct reads (get_document):'
Write-Output ('  {0,-32} {1,-7} {2,-7} {3,-7} {4,-7}' -f 'Document', 'alice', 'bob', 'carol', 'dave')
foreach ($title in ($falconIDs.Keys | Sort-Object)) {
    $cells = @()
    foreach ($caller in $callers) {
        $read = Invoke-DemoTool $tokens[$caller] 'get_document' @{document_id=$falconIDs[$title]; max_chars=200}
        $allowed = -not $read.isError
        if (-not $allowed -and ($read.content.text -join ' ') -ne 'resource not found') { throw "Unexpected error for $caller reading $title" }
        $cells += $(if ($allowed) { 'READ' } else { 'denied' })
        if ($allowed -ne ($expected[$caller] -contains $title)) { $failures += "$caller read of $title was $($cells[-1])" }
    }
    Write-Output ('  {0,-32} {1,-7} {2,-7} {3,-7} {4,-7}' -f $title, $cells[0], $cells[1], $cells[2], $cells[3])
}

Write-Output ''
if ($failures.Count -gt 0) { throw "FAIL: $($failures -join '; ')" }
Write-Output 'PASS: same query, four different result sets; each confidential Falcon document is readable by exactly one caller.'
