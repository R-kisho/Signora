/**
 * Signora - PDF stamping + Certificate of Completion
 *
 * 1. Copies the original PDF (the original file is never modified).
 * 2. Draws every signature inside its box. Box coordinates are percentages measured from the
 *    TOP-LEFT of the page (browser coordinates), so y is flipped for the PDF's BOTTOM-LEFT origin.
 * 3. Appends a Certificate of Completion (signers, IPs, document hash, full audit trail).
 *    It continues on extra pages when the audit trail is long.
 */
const fs = require('fs');
const { PDFDocument, rgb, StandardFonts } = require('pdf-lib');

// Standard PDF fonts only support basic Latin. Tamil / Sinhala names would crash drawText, so replace them.
const safe = (s) => String(s ?? '').replace(/[^\x20-\x7E]/g, '?');

// Shortens text with "..." so it never runs outside its column.
function fit(text, font, size, maxWidth) {
  let t = safe(text);
  if (font.widthOfTextAtSize(t, size) <= maxWidth) return t;
  while (t.length > 1 && font.widthOfTextAtSize(t + '...', size) > maxWidth) t = t.slice(0, -1);
  return t + '...';
}

async function stampPdf(payload) {
  const {
    originalPdfPath,
    outputPdfPath,
    boxes,
    signers,
    auditLogs,
    docTitle = 'Document',
    documentId = '',
    originalSha256 = '',
    completedAt = new Date().toISOString(),
  } = payload || {};

  if (typeof originalPdfPath !== 'string' || typeof outputPdfPath !== 'string' ||
      originalPdfPath === outputPdfPath || !fs.existsSync(originalPdfPath)) {
    throw new Error('A readable original PDF and a distinct output path are required');
  }
  if (!Array.isArray(boxes) || boxes.length === 0 || !Array.isArray(signers) ||
      signers.length === 0 || !Array.isArray(auditLogs)) {
    throw new Error('At least one signer, one signature box, and an audit log are required');
  }

  const signerById = new Map();
  for (const signer of signers) {
    if (!signer || typeof signer.id !== 'string' || !signer.id ||
        typeof signer.name !== 'string' || typeof signer.email !== 'string' ||
        !signer.signedAt || Number.isNaN(Date.parse(signer.signedAt)) ||
        signerById.has(signer.id)) {
      throw new Error('A signer record is missing valid identity or signing details');
    }
    signerById.set(signer.id, signer);
  }

  const pdfDoc = await PDFDocument.load(fs.readFileSync(originalPdfPath));
  const pages = pdfDoc.getPages();
  const pageSize = pages.map((page) => page.getSize());
  const fontRegular = await pdfDoc.embedFont(StandardFonts.Helvetica);
  const fontBold = await pdfDoc.embedFont(StandardFonts.HelveticaBold);

  // ---------- 1. Stamp signatures ----------
  const imageCache = new Map(); // one signer = one image, reused for all of their boxes
  const signersWithBoxes = new Set();
  for (const box of boxes) {
    if (!box || !Number.isInteger(box.page) || box.page < 1 || box.page > pages.length) {
      throw new Error(`Signature box has an invalid page number: ${box?.page}`);
    }
    const values = [box.x, box.y, box.width, box.height];
    if (!values.every(Number.isFinite) || box.x < 0 || box.y < 0 ||
        box.width <= 0 || box.height <= 0 ||
        box.x + box.width > 100 || box.y + box.height > 100) {
      throw new Error(`Signature box on page ${box.page} has invalid coordinates`);
    }
    const signer = signerById.get(box.signerId);
    if (!signer || signer.name !== box.signerName || signer.email !== box.signerEmail ||
        !box.signedAt || Number.isNaN(Date.parse(box.signedAt))) {
      throw new Error(`Signature box on page ${box.page} has no valid signed owner`);
    }
    const imageMatch = /^data:image\/(png|jpeg);base64,([A-Za-z0-9+/]+={0,2})$/.exec(box.signatureData || '');
    if (!imageMatch) {
      throw new Error(`Signature box on page ${box.page} has invalid signature image data`);
    }
    const imageBytes = Buffer.from(imageMatch[2], 'base64');
    if (imageBytes.length === 0 || imageBytes.toString('base64') !== imageMatch[2]) {
      throw new Error(`Signature box on page ${box.page} has invalid signature image data`);
    }

    const page = pages[box.page - 1];

    const { width: pw, height: ph } = page.getSize();
    if (!(pw > 0) || !(ph > 0) || !pageSize[box.page - 1]) {
      throw new Error(`PDF page ${box.page} has invalid dimensions`);
    }
    const boxW = (box.width / 100) * pw;
    const boxH = (box.height / 100) * ph;
    const boxX = (box.x / 100) * pw;
    const boxY = ph - ((box.y + box.height) / 100) * ph; // flip: top-left -> bottom-left origin

    let img = imageCache.get(box.signatureData);
    if (!img) {
      img = imageMatch[1] === 'jpeg'
        ? await pdfDoc.embedJpg(imageBytes)
        : await pdfDoc.embedPng(imageBytes);
      imageCache.set(box.signatureData, img);
    }
    const ratio = Math.min(boxW / img.width, boxH / img.height);
    const dw = img.width * ratio;
    const dh = img.height * ratio;
    page.drawImage(img, { x: boxX + (boxW - dw) / 2, y: boxY + (boxH - dh) / 2, width: dw, height: dh });

    const label = `Signed by ${box.signerName}${box.signedAt ? ' on ' + box.signedAt : ''}`;
    const labelY = boxY - 9 >= 2 ? boxY - 9 : Math.min(boxY + boxH + 2, ph - 9);
    const labelWidth = Math.min(Math.max(boxW, 140), pw - boxX);
    page.drawText(fit(label, fontRegular, 7, labelWidth), {
      x: boxX, y: labelY, size: 7, font: fontRegular, color: rgb(0.25, 0.25, 0.35),
    });
    signersWithBoxes.add(signer.id);
  }
  if (signersWithBoxes.size !== signerById.size) {
    throw new Error('Every signer must have at least one visible signature box');
  }

  // ---------- 2. Certificate of Completion ----------
  const A4 = [595.28, 841.89];
  const M = 36;
  const INDIGO = rgb(0.31, 0.27, 0.9);
  const DARK = rgb(0.1, 0.1, 0.15);
  const GREY = rgb(0.4, 0.45, 0.55);

  let page = pdfDoc.addPage(A4);
  const { width: W, height: H } = page.getSize();
  let y = H - 125;

  page.drawRectangle({ x: 0, y: H - 90, width: W, height: 90, color: INDIGO });
  page.drawText('SIGNORA', { x: M, y: H - 45, size: 20, font: fontBold, color: rgb(1, 1, 1) });
  page.drawText('Certificate of Completion & Audit Trail', { x: M, y: H - 68, size: 13, font: fontRegular, color: rgb(0.9, 0.9, 0.98) });

  const newPage = () => {
    page = pdfDoc.addPage(A4);
    page.drawText('SIGNORA - Certificate of Completion (continued)', { x: M, y: H - 40, size: 9, font: fontBold, color: INDIGO });
    y = H - 70;
  };
  const ensure = (need) => { if (y - need < 50) newPage(); };
  const text = (t, x, size, font = fontRegular, color = DARK) => page.drawText(safe(t), { x, y, size, font, color });

  // Document summary
  page.drawRectangle({ x: M, y: y - 82, width: W - 2 * M, height: 98, color: rgb(0.96, 0.97, 0.99), borderColor: rgb(0.85, 0.88, 0.94), borderWidth: 1 });
  text('DOCUMENT SUMMARY', 48, 9, fontBold, INDIGO);
  y -= 18; text(fit(`Title: ${docTitle}`, fontBold, 10, W - 2 * M - 24), 48, 10, fontBold);
  y -= 16; text(`Document ID: ${documentId}`, 48, 8, fontRegular, GREY);
  y -= 14; text(`Original file SHA-256: ${originalSha256}`, 48, 7, fontRegular, GREY);
  y -= 14; text(`Completed: ${new Date(completedAt).toUTCString()}`, 48, 8, fontRegular, GREY);
  y -= 40;

  // Signers
  text('SIGNATURE DETAILS', M, 11, fontBold);
  y -= 18;
  const list = signers.length ? signers : [];
  if (!list.length) { text('All required signers completed their signatures.', M, 9); y -= 18; }
  for (const s of list) {
    ensure(60);
    page.drawRectangle({ x: M, y: y - 44, width: W - 2 * M, height: 52, color: rgb(0.98, 0.99, 1), borderColor: rgb(0.88, 0.9, 0.95), borderWidth: 1 });
    y -= 10; text(fit(`${s.name || 'Signer'}  <${s.email || ''}>`, fontBold, 9, W - 2 * M - 24), 48, 9, fontBold);
    y -= 14; text(`Status: SIGNED   at ${s.signedAt ? new Date(s.signedAt).toUTCString() : 'n/a'}`, 48, 8, fontBold, rgb(0.1, 0.5, 0.2));
    y -= 12; text(`IP address: ${s.ip || 'not recorded'}`, 48, 8, fontRegular, GREY);
    y -= 26;
  }
  y -= 6;

  // Audit table
  ensure(60);
  text('AUDIT LOG TIMELINE', M, 11, fontBold);
  y -= 16;
  const COL = { time: M, action: 132, by: 236, details: 352 };
  const header = () => {
    text('Timestamp (UTC)', COL.time, 8, fontBold, GREY);
    text('Action', COL.action, 8, fontBold, GREY);
    text('Performed by', COL.by, 8, fontBold, GREY);
    text('Details / IP', COL.details, 8, fontBold, GREY);
    y -= 8;
    page.drawLine({ start: { x: M, y }, end: { x: W - M, y }, thickness: 0.5, color: rgb(0.8, 0.82, 0.88) });
    y -= 13;
  };
  header();
  for (const log of auditLogs) {
    if (y < 56) { newPage(); header(); }
    const when = log.createdAt ? new Date(log.createdAt).toISOString().replace('T', ' ').substring(0, 19) : '';
    const detail = [log.details, log.ipAddress ? `IP ${log.ipAddress}` : ''].filter(Boolean).join(' | ');
    text(when, COL.time, 7.5);
    text(fit(String(log.action || '').toUpperCase(), fontBold, 7.5, COL.by - COL.action - 6), COL.action, 7.5, fontBold, INDIGO);
    text(fit(log.performedBy, fontRegular, 7.5, COL.details - COL.by - 6), COL.by, 7.5);
    text(fit(detail, fontRegular, 7.5, W - M - COL.details), COL.details, 7.5, fontRegular, rgb(0.35, 0.35, 0.35));
    y -= 14;
  }

  // Footer on the last page
  page.drawText('Secured & verified by Signora Electronic Document Signing System.', { x: M, y: 25, size: 7, font: fontRegular, color: rgb(0.55, 0.6, 0.7) });

  fs.writeFileSync(outputPdfPath, await pdfDoc.save());
  console.log(`Successfully generated signed PDF at: ${outputPdfPath}`);
}

async function main() {
  const payloadPath = process.argv[2];
  if (!payloadPath) throw new Error('Usage: node stamp_pdf.js <payload_json_path>');
  await stampPdf(JSON.parse(fs.readFileSync(payloadPath, 'utf8')));
}

if (require.main === module) {
  main().catch((err) => {
    console.error('Fatal error during PDF stamping:', err);
    process.exitCode = 1;
  });
}

module.exports = { stampPdf };
