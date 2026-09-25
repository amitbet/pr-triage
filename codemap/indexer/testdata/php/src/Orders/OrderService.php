<?php

namespace Acme\Orders;

use Acme\Store\Repo;
use Acme\Store\PgRepo;
use function Acme\Util\clean;

/** Places orders. */
class OrderService
{
    private Repo $repo;

    public function __construct(?Repo $repo = null)
    {
        $this->repo = $repo ?? new PgRepo();
    }

    public function place(Order $order): int
    {
        if (!$this->validate($order)) {
            return 0;
        }
        clean($order);
        return $this->repo->save($order) + strlen(PgRepo::table());
    }

    private function validate(Order $order): bool
    {
        return $order->id > 0 && self::limit() > 0;
    }

    private static function limit(): int
    {
        return 10;
    }
}
