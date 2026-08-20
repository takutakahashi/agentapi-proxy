'use client';

import React, { createContext, useContext, useState, useEffect, useCallback, ReactNode } from 'react';

interface ThemeContextType {
  mainColor: string;
  setMainColor: (color: string) => void;
  resetToDefault: () => void;
}

const DEFAULT_MAIN_COLOR = '#3b82f6'; // Blue-500

const ThemeContext = createContext<ThemeContextType | undefined>(undefined);

export const useTheme = () => {
  const context = useContext(ThemeContext);
  if (!context) {
    throw new Error('useTheme must be used within a ThemeProvider');
  }
  return context;
};

interface ThemeProviderProps {
  children: ReactNode;
}

export const ThemeProvider: React.FC<ThemeProviderProps> = ({ children }) => {
  const [mainColor, setMainColorState] = useState<string>(DEFAULT_MAIN_COLOR);

  const updateCSSVariables = (color: string) => {
    if (typeof document !== 'undefined') {
      const root = document.documentElement;

      // Convert hex to RGB
      const hexToRgb = (hex: string) => {
        const result = /^#?([a-f\d]{2})([a-f\d]{2})([a-f\d]{2})$/i.exec(hex);
        return result ? {
          r: parseInt(result[1], 16),
          g: parseInt(result[2], 16),
          b: parseInt(result[3], 16)
        } : { r: 59, g: 130, b: 246 }; // Default blue
      };

      const rgb = hexToRgb(color);

      // Set CSS variables
      root.style.setProperty('--main-color', color);
      root.style.setProperty('--main-color-rgb', `${rgb.r}, ${rgb.g}, ${rgb.b}`);

      // Generate lighter and darker variants
      const lighterRgb = {
        r: Math.min(255, rgb.r + 40),
        g: Math.min(255, rgb.g + 40),
        b: Math.min(255, rgb.b + 40)
      };

      const darkerRgb = {
        r: Math.max(0, rgb.r - 40),
        g: Math.max(0, rgb.g - 40),
        b: Math.max(0, rgb.b - 40)
      };

      root.style.setProperty('--main-color-light', `rgb(${lighterRgb.r}, ${lighterRgb.g}, ${lighterRgb.b})`);
      root.style.setProperty('--main-color-dark', `rgb(${darkerRgb.r}, ${darkerRgb.g}, ${darkerRgb.b})`);

      // For ring/outline colors with opacity
      root.style.setProperty('--main-color-ring', `rgba(${rgb.r}, ${rgb.g}, ${rgb.b}, 0.5)`);
      root.style.setProperty('--main-color-bg', `rgba(${rgb.r}, ${rgb.g}, ${rgb.b}, 0.1)`);
    }
  };

  const setMainColor = useCallback((color: string) => {
    setMainColorState(color);
    updateCSSVariables(color);
  }, []);

  const resetToDefault = useCallback(() => {
    setMainColor(DEFAULT_MAIN_COLOR);
  }, [setMainColor]);

  // Initialize theme on mount with fixed default color
  useEffect(() => {
    updateCSSVariables(DEFAULT_MAIN_COLOR);
  }, []);

  const value: ThemeContextType = {
    mainColor,
    setMainColor,
    resetToDefault,
  };

  return (
    <ThemeContext.Provider value={value}>
      {children}
    </ThemeContext.Provider>
  );
};
