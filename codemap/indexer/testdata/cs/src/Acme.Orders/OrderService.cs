using Acme.Store;
using static Acme.Orders.Util;

namespace Acme.Orders;

public partial class OrderService
{
    private readonly IRepo _repo;

    public OrderService(IRepo repo)
    {
        _repo = repo ?? new PgRepo();
    }

    public int Count => _repo.Count;

    public object Place(Order o)
    {
        Validate(o);
        Audit(o.Id.Shout());
        var table = PgRepo.Table();
        var id = Clean(o.Id);
        return _repo.Save(o);
    }

    private void Validate(Order o)
    {
        if (o == null) throw new System.ArgumentNullException(nameof(o));
    }
}
