module Shop
  module Store
    class Repo
      def save(order)
        raise NotImplementedError
      end
    end

    class PgRepo < Repo
      def self.table = 'orders'

      def save(order)
        insert(order)
      end

      def insert(order) = order
    end
  end
end
