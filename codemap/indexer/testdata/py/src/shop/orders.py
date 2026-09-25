import shop.util
from shop.store import PgRepo, Repo


class OrderService:
    def __init__(self, repo: Repo):
        self.repo = repo
        self.cache = PgRepo()

    def place(self, order):
        self.validate(order)
        shop.util.clean(order)
        return self.repo.save(order)

    def validate(self, order, table=None):
        pass


def main():
    svc = OrderService(PgRepo())
    return svc.place({})
