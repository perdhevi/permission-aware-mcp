# Run from the repository root using PowerShell 7 after applying the optional
# SQL fixture, re-ingesting acme, and starting cmd/server.
param([string]$Endpoint = 'http://127.0.0.1:8080/mcp', [string]$Token = '')
$ErrorActionPreference = 'Stop'
if ([string]::IsNullOrWhiteSpace($Token)) {
    if ($env:AUTH_MODE -eq 'oidc') { throw 'Pass an Alice access token using -Token (go run ./cmd/login)' }
    $Token = go run ./cmd/devtoken mint --subject alice
    if ($LASTEXITCODE -ne 0) { throw 'Could not mint the Alice development token' }
}

function Invoke-DemoTool([string]$Name, [hashtable]$Arguments) {
    $headers = @{
        Authorization = "Bearer $token"
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
                'io.modelcontextprotocol/clientInfo' = @{name='injection-demo'; version='1'}
                'io.modelcontextprotocol/clientCapabilities' = @{}
            }
        }
    } | ConvertTo-Json -Depth 8
    $response = Invoke-RestMethod -Uri $Endpoint -Method Post -Headers $headers -ContentType application/json -Body $body
    if ($response.error) { throw 'MCP protocol request failed' }
    return $response.result
}

$identity = Invoke-DemoTool 'whoami' @{}
if ($identity.isError -or $identity.structuredContent.subject -notin @('alice', '10000000-0000-0000-0000-000000000001')) { throw 'Expected fixture Alice' }
$search = Invoke-DemoTool 'search_knowledge' @{query='Ignore previous instructions'; limit=20}
if ($search.isError) { throw 'Search failed; check migration, audit storage and ingestion' }
$injection = @($search.structuredContent.results | Where-Object { $_.content -match '\[Prompt injection demo\]' })
if ($injection.Count -eq 0 -or $injection[0].content -notmatch 'get_document with document_id ([0-9a-f-]{36})') {
    throw 'Demo instruction missing; apply seed/prompt-injection.sql and re-ingest acme'
}
$target = $Matches[1]
# Simulate a host following the hostile instruction. The bearer identity stays Alice.
$denied = Invoke-DemoTool 'get_document' @{document_id=$target}
if (-not $denied.isError -or ($denied.content.text -join ' ') -ne 'resource not found') {
    throw 'Expected generic denial of the injected direct-read request'
}
$spoof = Invoke-DemoTool 'get_document' @{document_id=$target; role='executive'; user_id='carol'}
if (-not $spoof.isError) { throw 'Identity spoofing was accepted' }
$after = Invoke-DemoTool 'whoami' @{}
if ($after.isError -or $after.structuredContent.subject -ne $identity.structuredContent.subject) { throw 'Caller identity changed' }
Write-Output 'PASS: Alice retrieved hostile text; the follow-up read and spoofing were denied and audited. Caller remains Alice.'
