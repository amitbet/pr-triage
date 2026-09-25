class Repo {
    [int] Save([int]$Id) { return 0 }
}

class PgRepo : Repo {
    static [string] Table() { return 'orders' }

    [int] Save([int]$Id) { return $this.Insert($Id) }

    hidden [int] Insert([int]$Id) { return $Id }
}
