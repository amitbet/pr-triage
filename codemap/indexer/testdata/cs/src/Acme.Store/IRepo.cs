namespace Acme.Store;

public interface IRepo
{
    int Count { get; }

    object Save(object o);
}
