import React from 'react';
import { render } from '@testing-library/react-native';
import { WebView } from 'react-native-webview';
import { LiveDriverTrackingMap } from '../src/components/LiveDriverTrackingMap';

function html(props: Record<string, unknown>): string {
  const r = render(<LiveDriverTrackingMap driverLatitude={19.07} driverLongitude={72.88} {...props} />);
  const out = r.UNSAFE_getByType(WebView as any).props.source.html as string;
  r.unmount();
  return out;
}

describe('LiveDriverTrackingMap', () => {
  // Claim 9a: the WebView script referenced the React theme object directly
  // (color: Colors.primary), which throws ReferenceError inside the WebView and
  // kills every marker after it.
  test('WebView script never references the React theme object', () => {
    expect(html({})).not.toMatch(/Colors\./);
  });

  // Claim 9b: without real trip coordinates the map used to draw a Mumbai→Pune
  // route regardless of where the trip actually was.
  test('draws no fabricated route when trip coordinates are unknown', () => {
    const out = html({});
    expect(out).not.toContain('18.95');
    expect(out).not.toContain('72.95');
    expect(out).not.toContain('73.85');
    // driver marker still drawn — only the fake legs disappear
    expect(out).toContain('var driver = [19.07, 72.88]');
  });

  test('draws the real legs when coordinates are supplied', () => {
    const out = html({
      pickupLatitude: 18.94,
      pickupLongitude: 72.93,
      destinationLatitude: 18.52,
      destinationLongitude: 73.86,
    });
    expect(out).toContain('var pickup = [18.94, 72.93]');
    expect(out).toContain('var dest = [18.52, 73.86]');
    expect(out).toMatch(/L\.marker\(pickup/);
    expect(out).toMatch(/L\.marker\(dest/);
  });

  // Claim 9c: the Leaflet bundle comes from unpkg; offline it never loads and
  // L.map() throws a ReferenceError, leaving a dead map with no explanation.
  test('renders an offline notice when the CDN bundle never arrives', () => {
    const out = html({});
    expect(out).toContain("typeof L === 'undefined'");
    expect(out).toContain('Map unavailable offline');
    // guard wraps the map init so the script does not die on L.map
    expect(out.indexOf("typeof L === 'undefined'")).toBeLessThan(out.indexOf('L.map('));
  });

  test('route line uses the interpolated theme colour, not a bare identifier', () => {
    expect(html({})).toContain("color: '#008069'");
  });
});
