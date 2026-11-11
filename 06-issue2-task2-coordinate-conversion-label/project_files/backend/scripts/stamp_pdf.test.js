const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const zlib = require('node:zlib');
const test = require('node:test');
const {
  PDFDocument, PDFArray, PDFDict, PDFName, PDFRawStream, decodePDFRawStream,
} = require('pdf-lib');
const { stampPdf } = require('./stamp_pdf');

function crc32(data) {
  let crc = 0xffffffff;
  for (const byte of data) {
    crc ^= byte;
    for (let bit = 0; bit < 8; bit++) {
      crc = (crc >>> 1) ^ (crc & 1 ? 0xedb88320 : 0);
    }
  }
  return (crc ^ 0xffffffff) >>> 0;
}

function pngChunk(type, data) {
  const typeBytes = Buffer.from(type);
  const length = Buffer.alloc(4);
  length.writeUInt32BE(data.length);
  const checksum = Buffer.alloc(4);
  checksum.writeUInt32BE(crc32(Buffer.concat([typeBytes, data])));
  return Buffer.concat([length, typeBytes, data, checksum]);
}

function signaturePng() {
  const width = 40;
  const height = 20;
  const header = Buffer.alloc(13);
  header.writeUInt32BE(width, 0);
  header.writeUInt32BE(height, 4);
  header[8] = 8;
  header[9] = 6;

  const pixels = Buffer.alloc(height * (width * 4 + 1));
  for (let y = 0; y < height; y++) {
    const row = y * (width * 4 + 1);
    for (let x = 4; x < 36; x++) {
      if (Math.abs(y - (5 + Math.floor(x / 8))) <= 1) {
        const pixel = row + 1 + x * 4;
        pixels[pixel] = 15;
        pixels[pixel + 1] = 23;
        pixels[pixel + 2] = 42;
        pixels[pixel + 3] = 255;
      }
    }
  }
  return Buffer.concat([
    Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]),
    pngChunk('IHDR', header),
    pngChunk('IDAT', zlib.deflateSync(pixels)),
    pngChunk('IEND', Buffer.alloc(0)),
  ]);
}

function validPayload(originalPdfPath, outputPdfPath, overrides = {}) {
  const signatureData = `data:image/png;base64,${signaturePng().toString('base64')}`;
  return {
    originalPdfPath,
    outputPdfPath,
    documentId: 'test-document',
    docTitle: 'Stamping test',
    originalSha256: 'test-hash',
    completedAt: '2026-01-01T00:00:00Z',
    signers: [{ id: 'signer-1', name: 'Test Signer', email: 'signer@example.test', signedAt: '2026-01-01T00:00:00Z' }],
    boxes: [{
      signerId: 'signer-1',
      page: 2,
      x: 10,
      y: 25,
      width: 30,
      height: 20,
      signerName: 'Test Signer',
      signerEmail: 'signer@example.test',
      signatureData,
      signedAt: '2026-01-01',
    }],
    auditLogs: [{
      action: 'DOCUMENT_SIGNED',
      performedBy: 'Test Signer (signer@example.test)',
      details: 'Signer submitted electronic signature',
      ipAddress: '192.0.2.1',
      createdAt: '2026-01-01T00:00:00Z',
    }],
    ...overrides,
  };
}

function pageContent(document, page) {
  const contents = document.context.lookup(page.node.Contents());
  const streams = contents instanceof PDFArray
    ? contents.asArray().map((ref) => document.context.lookup(ref, PDFRawStream))
    : [contents];
  return streams.map((stream) =>
    Buffer.from(decodePDFRawStream(stream).decode()).toString('latin1')
  ).join('\n');
}

test('stamps visible signature on requested page and appends completion certificate', async (t) => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'signora-stamp-test-'));
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
  const inputPath = path.join(dir, 'original.pdf');
  const outputPath = path.join(dir, 'signed.pdf');

  const original = await PDFDocument.create();
  original.addPage([300, 400]);
  original.addPage([600, 300]);
  fs.writeFileSync(inputPath, await original.save());

  await stampPdf(validPayload(inputPath, outputPath));
  const signedBytes = fs.readFileSync(outputPath);
  const signed = await PDFDocument.load(signedBytes);

  assert.equal(signed.getPageCount(), 3);
  const certificateContent = pageContent(signed, signed.getPage(2));
  assert.ok(certificateContent.includes('<4365727469666963617465206F6620436F6D706C6574696F6E'));
  assert.ok(certificateContent.includes('<444F43554D454E545F5349474E4544'));

  const page = signed.getPage(1);
  const resources = signed.context.lookup(page.node.Resources(), PDFDict);
  const xObjects = resources.lookup(PDFName.of('XObject'), PDFDict);
  assert.ok(xObjects, 'signed page contains image resources');
  assert.equal(xObjects.keys().length, 1, 'signature image is embedded on the selected page');

  const content = pageContent(signed, page);
  assert.match(content, /1 0 0 1 90 165 cm/, 'top-left page coordinates are converted and Y is inverted');
  assert.match(content, /120 0 0 60 0 0 cm/, 'signature image is fitted to the signature box');
});

test('rejects missing signature images and invalid page numbers', async (t) => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'signora-stamp-errors-'));
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
  const inputPath = path.join(dir, 'original.pdf');
  const pdf = await PDFDocument.create();
  pdf.addPage([300, 400]);
  fs.writeFileSync(inputPath, await pdf.save());

  const missing = validPayload(inputPath, path.join(dir, 'missing.pdf'), {
    boxes: [{ ...validPayload(inputPath, '').boxes[0], page: 1, signatureData: '' }],
  });
  await assert.rejects(stampPdf(missing), /invalid signature image data/);

  const invalidPage = validPayload(inputPath, path.join(dir, 'invalid-page.pdf'), {
    boxes: [{ ...validPayload(inputPath, '').boxes[0], page: 2 }],
  });
  await assert.rejects(stampPdf(invalidPage), /invalid page number/);
});
