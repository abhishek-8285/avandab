/**
 * Client-Side Image Compressor for Mobile / 4G Uploads
 * Downsamples camera images (>1600px, >300KB) to high-efficiency JPEG (~120-200KB)
 * using HTML5 Canvas before network transmission.
 */
(function() {
  'use strict';

  function formatBytes(bytes) {
    if (bytes === 0) return '0 B';
    var k = 1024;
    var sizes = ['B', 'KB', 'MB'];
    var i = Math.floor(Math.log(bytes) / Math.log(k));
    return (bytes / Math.pow(k, i)).toFixed(1) + ' ' + sizes[i];
  }

  function compressImage(file, maxDim, quality) {
    return new Promise(function(resolve) {
      if (!file.type || !file.type.startsWith('image/') || file.size < 300 * 1024) {
        return resolve(file);
      }

      var img = new Image();
      var url = URL.createObjectURL(file);
      img.onload = function() {
        URL.revokeObjectURL(url);
        var w = img.naturalWidth || img.width;
        var h = img.naturalHeight || img.height;
        maxDim = maxDim || 1600;
        quality = quality || 0.82;

        if (w <= maxDim && h <= maxDim && file.size < 500 * 1024) {
          return resolve(file);
        }

        if (w > h) {
          if (w > maxDim) {
            h = Math.round((h * maxDim) / w);
            w = maxDim;
          }
        } else {
          if (h > maxDim) {
            w = Math.round((w * maxDim) / h);
            h = maxDim;
          }
        }

        var canvas = document.createElement('canvas');
        canvas.width = w;
        canvas.height = h;
        var ctx = canvas.getContext('2d');
        if (!ctx) return resolve(file);

        ctx.imageSmoothingEnabled = true;
        ctx.imageSmoothingQuality = 'high';
        ctx.drawImage(img, 0, 0, w, h);

        canvas.toBlob(function(blob) {
          if (!blob) return resolve(file);
          var baseName = file.name ? file.name.replace(/\.[^.]+$/, '') : 'photo';
          var compressed = new File([blob], baseName + '.jpg', {
            type: 'image/jpeg',
            lastModified: Date.now()
          });
          resolve(compressed);
        }, 'image/jpeg', quality);
      };
      img.onerror = function() {
        URL.revokeObjectURL(url);
        resolve(file);
      };
      img.src = url;
    });
  }

  function attachToInput(input) {
    if (input._compressAttached) return;
    input._compressAttached = true;

    input.addEventListener('change', async function() {
      if (!input.files || input.files.length === 0) return;
      var file = input.files[0];
      if (!file.type || !file.type.startsWith('image/')) return;
      if (file.size < 300 * 1024) return;

      var origSize = file.size;
      try {
        var compressed = await compressImage(file, 1600, 0.82);
        if (compressed.size < origSize && window.DataTransfer) {
          var dt = new DataTransfer();
          dt.items.add(compressed);
          input.files = dt.files;

          var badge = input.parentElement ? input.parentElement.querySelector('.compress-badge') : null;
          if (!badge && input.parentElement) {
            badge = document.createElement('div');
            badge.className = 'compress-badge text-xs text-emerald-600 dark:text-emerald-400 mt-1.5 flex items-center gap-1 font-medium';
            input.parentElement.appendChild(badge);
          }
          if (badge) {
            badge.textContent = '⚡ Compressed for 4G: ' + formatBytes(origSize) + ' → ' + formatBytes(compressed.size);
          }
        }
      } catch (err) {
        console.warn('Image compression fallback:', err);
      }
    });
  }

  function init() {
    var inputs = document.querySelectorAll('input[type="file"]');
    for (var i = 0; i < inputs.length; i++) {
      attachToInput(inputs[i]);
    }
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', init);
  } else {
    init();
  }
  document.addEventListener('htmx:afterSwap', init);
})();
