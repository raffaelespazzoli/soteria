import { formatElapsedMs } from '../../src/hooks/useElapsedTime';

describe('formatElapsedMs', () => {
  it('returns "0s" for negative values', () => {
    expect(formatElapsedMs(-1000)).toBe('0s');
  });

  it('returns "0s" for NaN', () => {
    expect(formatElapsedMs(NaN)).toBe('0s');
  });

  it('returns "0s" for Infinity', () => {
    expect(formatElapsedMs(Infinity)).toBe('0s');
  });

  it('formats seconds', () => {
    expect(formatElapsedMs(45000)).toBe('45s');
  });

  it('formats minutes and seconds', () => {
    expect(formatElapsedMs(125000)).toBe('2m 5s');
  });

  it('formats hours and minutes', () => {
    expect(formatElapsedMs(3725000)).toBe('1h 2m');
  });

  it('formats zero', () => {
    expect(formatElapsedMs(0)).toBe('0s');
  });

  it('formats exactly 60 seconds as "1m 0s"', () => {
    expect(formatElapsedMs(60000)).toBe('1m 0s');
  });

  it('formats exactly 1 hour as "1h 0m"', () => {
    expect(formatElapsedMs(3600000)).toBe('1h 0m');
  });
});
