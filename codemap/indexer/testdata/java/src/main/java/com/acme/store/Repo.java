package com.acme.store;

public interface Repo<T> {
    T save(T item);
}
