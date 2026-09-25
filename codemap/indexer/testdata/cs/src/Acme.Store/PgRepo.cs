namespace Acme.Store;

public class PgRepo : IRepo
{
    public static string Table() => "orders";

    public int Count { get; private set; }

    public object Save(object o)
    {
        Count++;
        return Insert(o);
    }

    private object Insert(object o) => o;
}
