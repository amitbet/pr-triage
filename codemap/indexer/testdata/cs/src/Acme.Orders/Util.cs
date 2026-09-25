namespace Acme.Orders
{
    public static class Util
    {
        public static string Clean(string s) => s.Trim();

        public static string Shout(this string s) => s.ToUpper();
    }
}
