import { expect, waitUntil } from '@open-wc/testing';
import htmx from 'htmx.org';
import '../../main';

// The answers of the settings screen that change a table (the models of a local endpoint, the MCP tokens) put table rows
// in a <template>: an HTML parser drops a <tr> / <tbody> that is not first or inside one, and the OOB swap of htmx reads
// the template. This is what spec/components/settings-screen-routes.md §2 needs of the browser.
describe('table rows answered out of band (settings screen)', () => {
  let host: HTMLElement;

  beforeEach(() => {
    host = document.createElement('div');
    host.innerHTML = `
      <table><tbody id="rows">
        <tr id="rows-empty"><td>まだありません</td></tr>
        <tr id="row-b"><th scope="row">b</th><td></td></tr>
      </tbody></table>
      <p id="rows-note">waiting</p>
      <button type="button" id="go" hx-get="/src/components/testdata/settings-table-rows.html" hx-target="#rows-note" hx-swap="none">go</button>`;
    document.body.append(host);
    htmx.process(host);
  });
  afterEach(() => host.remove());

  it('replaces a row, appends a new one to the table body and takes the empty row away', async () => {
    host.querySelector<HTMLButtonElement>('#go')!.click();
    await waitUntil(() => host.querySelector('#row-c') !== null, 'the new row was not added');

    expect(host.querySelector('#rows-empty')).to.equal(null);
    expect(host.querySelector('#row-b')!.textContent).to.contain('置き換え後');
    expect(host.querySelector('#rows > tr#row-c')!.textContent).to.contain('追加');
    expect(host.querySelectorAll('#rows > tr').length).to.equal(2);
    expect(host.querySelector('#rows-note')!.textContent).to.equal('answered');
  });
});
