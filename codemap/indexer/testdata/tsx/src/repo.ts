export class Repo {
  save(x: number) {
    return x;
  }
  static open(): Repo {
    return new Repo();
  }
}
