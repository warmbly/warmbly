export const timeOptions = Array.from({ length: 48 }, (_, i) => {
  const hour24 = Math.floor(i / 2);
  const minute = (i % 2) * 30;

  const h12 = hour24 % 12 === 0 ? 12 : hour24 % 12;
  const ampm = hour24 < 12 ? 'AM' : 'PM';

  const name = `${h12.toString().padStart(2, "0")}:${minute.toString().padStart(2, '0')} ${ampm}`;
  const value = `${hour24.toString().padStart(2, '0')}:${minute.toString().padStart(2, '0')}`;

  return { name, value };
});

export function to12Hour(time24: string): string {
  const [h, m] = time24.split(':').map(Number);
  const suffix = h >= 12 ? 'PM' : 'AM';
  const h12 = h % 12 === 0 ? 12 : h % 12;
  return `${h12.toString().padStart(2, "0")}:${m.toString().padStart(2, '0')} ${suffix}`;
}