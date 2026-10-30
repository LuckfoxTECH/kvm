import { useState , useEffect } from "react";
import { Select } from "antd";
import {useReactAt} from 'i18n-auto-extractor/react'
import { isMobile } from "react-device-detect";

import { SettingsPageHeader } from "@components/Settings/SettingsPageheader";
import { useSettingsStore } from "@/hooks/stores";
import type { FullscreenMode } from "@/hooks/stores";
import { SettingsItem } from "@components/Settings/SettingsView";
import enJSON from '@/locales/en.json';
import zhJSON from '@/locales/zh.json';
import { ThemeMode, useTheme } from "@/layout/contexts/ThemeContext";
import { dark_font_style } from "@/layout/theme_color";

const { Option } = Select;


export default function SettingsGeneral() {
  const { $at, setCurrentLang } = useReactAt();

  // Theme and Language State
  const [theme, setTheme] = useState<string>('light');
  const { setThemeMode } = useTheme();
  const language = useSettingsStore(state => state.language);
  const setLanguage = useSettingsStore(state => state.setLanguage);
  const fullscreenMode = useSettingsStore(state => state.fullscreenMode);
  const setFullscreenMode = useSettingsStore(state => state.setFullscreenMode);

  const handleFullscreenModeChange = (value: string) => {
    setFullscreenMode(value as FullscreenMode);
  };

  // Language Change Handler
  const handleLanguageChange = (value: string) => {
    setLanguage(value);
    setCurrentLang(value, value === 'en' ? enJSON : zhJSON);
  };

  // Initialize Language
  useEffect(() => {
    setCurrentLang(language, language === 'en' ? enJSON : zhJSON);
  }, [language, setCurrentLang]);

  // Theme Change Handler
  const handleThemeChange = (value: string) => {
    const root = document.documentElement;
    setThemeMode(value as ThemeMode)
    
    localStorage.setItem('theme', value);
    root.classList.remove('light', 'dark');
    root.classList.add(value);
    

    setTheme(value);
  };

  // Initialize Theme
  useEffect(() => {
    const savedTheme = localStorage.getItem('theme') || 'light';
    setTheme(savedTheme);

    const root = document.documentElement;
    
    root.classList.remove('light', 'dark');
    root.classList.add(savedTheme);
     
  }, [theme]);

  return (
    <div className="space-y-4 pb-[50px]">
      <SettingsPageHeader
        title={$at("General")}
        description={$at("Configure device settings and update preferences")}
      />
      <div className="space-y-4">
        <SettingsItem
          title={$at("Theme")}
          description={$at("Choose your preferred color theme")}
          className={`${isMobile ? "w-full flex-col" : ""}`}
        >
          <div className={`space-y-2 ${isMobile ? "w-full" : "w-[37%]"}`}>
            <Select
              value={theme}
              onChange={handleThemeChange}
              className={`!w-full !h-[36px]`}
            >
              <Option value="light" className={dark_font_style}>{$at('Light')}</Option>
              <Option value="dark" className={dark_font_style}>{$at('Dark')}</Option>
            </Select>
          </div>
        </SettingsItem>
      </div>

      <div className="space-y-4">
        <SettingsItem
          title={$at("Language")}
          description={$at("Choose your language")}
          className={`${isMobile ? "w-full flex-col" : ""}`}
        >
          <div className={`space-y-2 ${isMobile ? "w-full" : "w-[37%]"}`}>
            <Select
              value={language}
              onChange={handleLanguageChange}
              className={`!w-full !h-[36px]`}
            >
              <Option value="en" className={dark_font_style}>{$at('English')}</Option>
              <Option value="zh" className={dark_font_style}>{$at('中文')}</Option>
            </Select>
          </div>
        </SettingsItem>
      </div>

      <div className="space-y-4">
        <SettingsItem
          title={$at("Fullscreen Mode")}
          description={$at(
            "Choose how the fullscreen button behaves. Full page fullscreen is not supported on iOS, use Video Only there.",
          )}
          className={`${isMobile ? "w-full flex-col" : ""}`}
        >
          <div className={`space-y-2 ${isMobile ? "w-full" : "w-[37%]"}`}>
            <Select
              value={fullscreenMode}
              onChange={handleFullscreenModeChange}
              className={`!w-full !h-[36px]`}
            >
              <Option value="page" className={dark_font_style}>{$at('Whole Page')}</Option>
              <Option value="video" className={dark_font_style}>{$at('Video Only')}</Option>
            </Select>
          </div>
        </SettingsItem>
      </div>

    </div>
  );
}
