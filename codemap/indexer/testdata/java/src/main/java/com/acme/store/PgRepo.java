package com.acme.store;

public class PgRepo<T> implements Repo<T> {
    @Override
    public T save(T item) {
        return item;
    }

    public static String table() { return "t"; }
}
