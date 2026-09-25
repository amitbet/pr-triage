<?php

namespace Acme\Store;

use Acme\Orders\Order;

interface Repo
{
    public function save(Order $order): int;
}
