<?php

namespace Acme\Store;

use Acme\Orders\Order;

class PgRepo implements Repo
{
    public static function table(): string
    {
        return 'orders';
    }

    public function save(Order $order): int
    {
        return $this->insert($order);
    }

    protected function insert(Order $order): int
    {
        return $order->id;
    }
}
