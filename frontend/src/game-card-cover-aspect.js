export function naturalCoverAspect(width, height) {
  const naturalWidth = Number(width);
  const naturalHeight = Number(height);
  if (!Number.isFinite(naturalWidth) || !Number.isFinite(naturalHeight) || naturalWidth <= 0 || naturalHeight <= 0) {
    return "";
  }
  return `${naturalWidth} / ${naturalHeight}`;
}

export function bindNaturalCoverAspect(coverBox, cover) {
  const image = cover?.querySelector?.("img");
  if (!image || !coverBox?.style?.setProperty) return;

  const apply = () => {
    const aspect = naturalCoverAspect(image.naturalWidth, image.naturalHeight);
    if (aspect) coverBox.style.setProperty("--lib-cover-aspect", aspect);
  };

  image.addEventListener("load", apply, { once: true });
  if (image.complete) apply();
}
