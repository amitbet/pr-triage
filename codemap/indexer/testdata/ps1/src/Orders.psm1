. "$PSScriptRoot/Private/Repo.ps1"

# Places an order.
function Invoke-Order {
    param([int]$Id)
    if (-not (Test-Order -Id $Id)) {
        return 0
    }
    $repo = [PgRepo]::new()
    return $repo.Save($Id) + [PgRepo]::Table()
}

function Test-Order {
    param([int]$Id)
    return $Id -gt 0
}
