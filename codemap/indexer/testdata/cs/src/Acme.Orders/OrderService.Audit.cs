namespace Acme.Orders;

public partial class OrderService
{
    private void Audit(string msg) => System.Console.WriteLine(msg + Count);
}
