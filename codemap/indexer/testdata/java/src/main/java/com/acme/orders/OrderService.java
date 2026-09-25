package com.acme.orders;

import com.acme.store.Repo;
import com.acme.store.PgRepo;
import static com.acme.orders.Util.clean;

public class OrderService {
    private final Repo<Order> repo;

    public OrderService() {
        this.repo = new PgRepo<>();
    }

    public Order place(Order o) {
        validate(o);
        String t = PgRepo.table();
        return repo.save(clean(o));
    }

    private void validate(Order o) {}
}
