TABLE = "orders"
item = None


class Repo:
    def save(self, item):
        raise NotImplementedError


class PgRepo(Repo):
    def save(self, item):
        return insert(TABLE, item)


def insert(table, item):
    TABLE = table
    return item, TABLE
