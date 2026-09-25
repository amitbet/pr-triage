import { label } from './util';
import { make } from './svc';

export const View = () => (
  <section title="label">
    make sure label is only text here
    {label('x')}
  </section>
);
