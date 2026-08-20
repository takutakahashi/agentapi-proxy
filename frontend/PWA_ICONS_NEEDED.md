# PWA Icons Required

The PWA implementation requires the following icon files to be created and placed in the `/public` directory:

## Required Icons:

1. **icon-192x192.png** - 192x192 pixels
   - Purpose: App icon for Android and general use
   - Format: PNG
   - Should be maskable (safe zone design)

2. **icon-256x256.png** - 256x256 pixels
   - Purpose: Medium-size app icon
   - Format: PNG

3. **icon-384x384.png** - 384x384 pixels
   - Purpose: Large app icon for high-density screens
   - Format: PNG

4. **icon-512x512.png** - 512x512 pixels
   - Purpose: Largest app icon, used for splash screens
   - Format: PNG
   - Should be maskable (safe zone design)

5. **favicon.ico** - Standard favicon
   - Purpose: Browser tab icon
   - Format: ICO (16x16, 32x32, 48x48 sizes included)

## Design Guidelines:

- Use the AgentAPI branding/colors
- Ensure icons are readable at small sizes
- For maskable icons (192x192 and 512x512), keep important content within the safe zone (center 80% of the canvas)
- Use a consistent design across all sizes
- Consider dark/light mode compatibility

## Tools for Icon Generation:

- [PWA Builder](https://www.pwabuilder.com/imageGenerator) - Free PWA icon generator
- [Favicon Generator](https://favicon.io/) - For favicon.ico
- Design tools like Figma, Sketch, or Adobe Illustrator

Replace the placeholder files in `/public` with actual PNG images of the specified dimensions.