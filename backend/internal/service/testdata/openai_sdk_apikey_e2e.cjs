// Drives the official OpenAI Node SDK against the gateway test server started
// by TestOpenAINodeSDKAgainstAPIKeyAccount (an OpenAI API-key / relay account).
// Usage: node openai_sdk_apikey_e2e.cjs <sdkDir> <relayBaseURL> <officialBaseURL>
const OpenAI = require(process.argv[2]);
const toFile = OpenAI.toFile || require(process.argv[2] + '/uploads').toFile;

const client = new OpenAI({ apiKey: 'sk-test', baseURL: process.argv[3], maxRetries: 0, timeout: 20000 });
const PNG = 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==';
const report = { sdkVersion: require(process.argv[2] + '/package.json').version, cases: {} };

async function run(name, fn) {
  try {
    report.cases[name] = { ok: true, ...(await fn()) };
  } catch (err) {
    report.cases[name] = { ok: false, error: String((err && err.stack) || err) };
  }
}

(async () => {
  await run('images_generate_26', async () => {
    const res = await client.images.generate({
      model: 'gpt-image-2.6-flare',
      prompt: 'case=img_generate a red apple, pixel art',
      size: '1024x1024',
      quality: 'low',
      complexity: 'low',
      background: 'transparent',
      output_format: 'png',
      n: 1,
    });
    return { count: res.data.length, b64: res.data[0].b64_json, size: res.size, quality: res.quality, usage: res.usage };
  });

  await run('images_edit_26', async () => {
    const res = await client.images.edit({
      model: 'gpt-image-2.6-sunburst',
      image: await toFile(Buffer.from(PNG, 'base64'), 'ref.png', { type: 'image/png' }),
      prompt: 'case=img_edit make the apple blue',
      size: '1024x1024',
      quality: 'medium',
      complexity: 'medium',
    });
    return { count: res.data.length, b64: res.data[0].b64_json, size: res.size };
  });

  const collect = async (stream) => {
    const types = [];
    let last = {};
    for await (const event of stream) {
      types.push(event.type);
      last = event;
    }
    return { types: types.join(','), b64: last.b64_json, usage: last.usage };
  };

  await run('images_stream_26', async () => collect(await client.images.generate({
    model: 'gpt-image-2.6-flare',
    prompt: 'case=img_stream a red apple, pixel art',
    size: '1024x1024',
    quality: 'low',
    stream: true,
    partial_images: 2,
  })));

  await run('images_edit_stream_26', async () => collect(await client.images.edit({
    model: 'gpt-image-2.6-sunburst',
    image: await toFile(Buffer.from(PNG, 'base64'), 'ref.png', { type: 'image/png' }),
    prompt: 'case=img_edit_stream make the apple blue',
    stream: true,
    partial_images: 1,
  })));

  await run('images_relay_response_format', async () => {
    const res = await client.images.generate({ model: 'gpt-image-2.6-flare', prompt: 'case=img_relay_format apple', response_format: 'b64_json' });
    return { b64: res.data[0].b64_json };
  });

  // The second gateway server routes to an API-key account without base_url,
  // i.e. the official API, which rejects response_format for GPT Image models.
  const official = new OpenAI({ apiKey: 'sk-test', baseURL: process.argv[4], maxRetries: 0, timeout: 20000 });

  await run('images_official_url', async () => {
    const res = await official.images.generate({ model: 'gpt-image-2.6-flare', prompt: 'case=img_official_url apple', response_format: 'url' });
    return { url: res.data[0].url, b64: res.data[0].b64_json };
  });

  await run('images_official_edit_b64', async () => {
    const res = await official.images.edit({
      model: 'gpt-image-2.6-flare',
      image: await toFile(Buffer.from(PNG, 'base64'), 'ref.png', { type: 'image/png' }),
      prompt: 'case=img_official_edit make it blue',
      response_format: 'b64_json',
    });
    return { b64: res.data[0].b64_json };
  });

  await run('images_official_stream', async () => collect(await official.images.generate({
    model: 'gpt-image-2.6-flare',
    prompt: 'case=img_official_stream apple',
    response_format: 'b64_json',
    stream: true,
    partial_images: 1,
  })));

  await run('responses_retrieve_delete', async () => {
    const first = await client.responses.create({ model: 'gpt-5.4', input: 'case=apikey_first remember 7' });
    const fetched = await client.responses.retrieve(first.id);
    const items = await client.responses.inputItems.list(first.id, { limit: 5 });
    const second = await client.responses.create({ model: 'gpt-5.4', previous_response_id: first.id, input: 'case=apikey_second what number?' });
    const deleted = await client.responses.delete(first.id);
    let afterDeleteStatus = 0;
    try {
      await client.responses.retrieve(first.id);
    } catch (err) {
      afterDeleteStatus = err.status || -1;
    }
    let foreignStatus = 0;
    try {
      await client.responses.retrieve('resp_unknown_elsewhere');
    } catch (err) {
      foreignStatus = err.status || -1;
    }
    return {
      firstID: first.id, fetchedID: fetched.id, fetchedText: fetched.output_text,
      inputItemCount: items.data.length, inputItemText: items.data[0] && items.data[0].content && items.data[0].content[0] && items.data[0].content[0].text,
      secondText: second.output_text, secondPrev: second.previous_response_id,
      deleted: deleted === undefined ? 'void' : deleted, afterDeleteStatus, foreignStatus,
    };
  });

  await run('responses_stream_apikey', async () => {
    const stream = client.responses.stream({ model: 'gpt-5.4', input: 'case=apikey_stream say hello' });
    let deltas = '';
    stream.on('response.output_text.delta', (e) => { deltas += e.delta; });
    const final = await stream.finalResponse();
    return { deltas, outputText: final.output_text, id: final.id };
  });

  process.stdout.write('SDK_REPORT ' + JSON.stringify(report) + '\n');
})();
