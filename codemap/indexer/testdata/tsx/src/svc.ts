import { Repo } from './repo';
import { audit } from './util';

export class Svc {
  constructor(private repo: Repo) {}
  run(x: number) {
    return this.store(x);
  }
  store(x: number) {
    return this.repo.save(x);
  }
}

export function make() {
  return new Svc(Repo.open());
}

audit(make);
