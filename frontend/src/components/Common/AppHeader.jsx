import React, { useState, useEffect, useRef } from "react";
import {
  Search,
  X,
  Moon,
  Sun,
  Monitor,
  Menu,
  MoreVertical,
  Check,
  LogOut,
} from "lucide-react";
import { useNavigate } from "react-router-dom";
import { useAuth } from "../../context/AuthContext";
import { useTheme } from "../../context/ThemeContext";

const AppHeader = ({
  onInscribe,
  showSearch = false,
  searchQuery = "",
  onSearchChange,
  onClearSearch,
  renderInlineSearch,
  showBrcToggle = false,
  hideBrc20 = true,
  onToggleBrc20,
  showThemeToggle = true,
  isDarkMode: propIsDarkMode,
}) => {
  const navigate = useNavigate();
  const { auth, signOut } = useAuth();
  const themeContext = useTheme();

  const isDarkMode = themeContext
    ? themeContext.isDarkMode
    : propIsDarkMode || false;
  const useSystemTheme = themeContext ? themeContext.useSystemTheme : false;
  const setTheme = themeContext ? themeContext.setTheme : () => {};

  const cycleTheme = () => {
    if (useSystemTheme) {
      setTheme("light");
    } else if (isDarkMode) {
      setTheme("auto");
    } else {
      setTheme("dark");
    }
  };

  const getThemeIcon = () => {
    if (useSystemTheme) return <Monitor className="w-5 h-5" />;
    return isDarkMode ? (
      <Moon className="w-5 h-5" />
    ) : (
      <Sun className="w-5 h-5" />
    );
  };

  const getThemeTitle = () => {
    if (useSystemTheme) return "Theme: Auto (click for light)";
    return isDarkMode
      ? "Theme: Dark (click for auto)"
      : "Theme: Light (click for dark)";
  };

  const [isMenuOpen, setIsMenuOpen] = useState(false);
  const [isDropdownOpen, setIsDropdownOpen] = useState(false);
  const dropdownRef = useRef(null);

  useEffect(() => {
    const handleClickOutside = (event) => {
      if (dropdownRef.current && !dropdownRef.current.contains(event.target)) {
        setIsDropdownOpen(false);
      }
    };

    if (isDropdownOpen) {
      document.addEventListener("mousedown", handleClickOutside);
    }

    return () => {
      document.removeEventListener("mousedown", handleClickOutside);
    };
  }, [isDropdownOpen]);

  return (
    <header className="nav-glass fixed top-0 left-0 right-0 z-50">
      <nav className="starlight-nav bg-transparent border-none w-full">
        <div className="container mx-auto px-6 h-16 flex flex-row items-center justify-between">
          {/* Left Side: Logo & Links */}
          <div className="flex flex-row items-center gap-8">
            <button
              onClick={() => navigate("/")}
              className="flex flex-row items-center gap-3 p-0 bg-transparent border-none cursor-pointer group"
            >
              <div className="flex items-center justify-center w-8 h-8 bg-starlight rounded-lg glow-blue group-hover:scale-105 transition-transform">
                <span className="text-white text-lg font-extrabold">✦</span>
              </div>
              <h1 className="text-2xl font-bold text-gradient-starlight m-0">
                Starlight
              </h1>
            </button>

            <div className="nav-desktop">
              <ul className="nav-list flex flex-row items-center">
                <li>
                  <button onClick={onInscribe} className="nav-link">
                    Inscribe
                  </button>
                </li>
                <li>
                  <button onClick={() => navigate("/")} className="nav-link">
                    Blocks
                  </button>
                </li>
                <li>
                  <button
                    onClick={() => navigate("/contracts")}
                    className="nav-link"
                  >
                    Contracts
                  </button>
                </li>
                <li>
                  <button
                    onClick={() => navigate("/discover")}
                    className="nav-link"
                  >
                    Discover
                  </button>
                </li>
                <li>
                  <button
                    onClick={() => navigate("/docs")}
                    className="nav-link"
                  >
                    Documents
                  </button>
                </li>
              </ul>
            </div>
          </div>

          {/* Right Side: Search & Actions */}
          <div className="flex flex-row items-center gap-4">
            <div className="nav-desktop">
              <div className="nav-actions flex flex-row items-center gap-2 h-full">
                {showSearch && (
                  <div className="search has-icon mr-2">
                    <Search className="icon-search w-4 h-4" />
                    <input
                      type="text"
                      placeholder="Search..."
                      value={searchQuery}
                      onChange={(e) => onSearchChange?.(e.target.value)}
                      onKeyDown={(e) => {
                        if (e.key === "Escape") {
                          onClearSearch?.();
                        }
                      }}
                      className="input search-input text-sm"
                    />
                    {searchQuery && (
                      <button onClick={onClearSearch} className="search-clear">
                        <X className="w-4 h-4" />
                      </button>
                    )}
                    {renderInlineSearch && renderInlineSearch()}
                  </div>
                )}

                {showThemeToggle && (
                  <button
                    onClick={cycleTheme}
                    className="nav-link px-2 bg-transparent border-none cursor-pointer"
                    title={getThemeTitle()}
                  >
                    {getThemeIcon()}
                  </button>
                )}

                <div
                  className={`dropdown h-full flex items-center ${isDropdownOpen ? "active" : ""}`}
                  ref={dropdownRef}
                >
                  <button
                    onClick={() => setIsDropdownOpen(!isDropdownOpen)}
                    className="nav-link px-2 bg-transparent border-none cursor-pointer"
                    title="More options"
                  >
                    <MoreVertical className="w-5 h-5" />
                  </button>
                  <div
                    className="dropdown-menu active"
                    style={{ right: 0, left: "auto" }}
                  >
                    {showBrcToggle && (
                      <button
                        onClick={() => {
                          onToggleBrc20?.();
                          setIsDropdownOpen(false);
                        }}
                        className="dropdown-item flex flex-row items-center justify-between"
                      >
                        <span>Hide BRC-20</span>
                        {hideBrc20 && (
                          <Check className="w-4 h-4 text-primary" />
                        )}
                      </button>
                    )}
                    {auth?.apiKey ? (
                      <>
                        <div className="px-4 py-2 border-t border-white/5">
                          <div className="text-[10px] text-muted mb-1 uppercase tracking-widest font-bold">
                            Wallet
                          </div>
                          <div className="badge-success text-[11px] px-2 py-0.5 rounded truncate w-full text-center">
                            {auth.wallet || auth.email || "Connected"}
                          </div>
                        </div>
                        <button
                          onClick={() => {
                            signOut();
                            setIsDropdownOpen(false);
                          }}
                          className="dropdown-item text-error border-t border-white/5"
                        >
                          Sign out
                        </button>
                      </>
                    ) : (
                      <button
                        onClick={() => {
                          navigate("/auth");
                          setIsDropdownOpen(false);
                        }}
                        className="dropdown-item border-t border-white/5"
                      >
                        Sign In
                      </button>
                    )}
                  </div>
                </div>
              </div>
            </div>

            <button
              onClick={() => setIsMenuOpen(!isMenuOpen)}
              className={`hamburger md:hidden ${isMenuOpen ? "active" : ""} bg-transparent border-none`}
              aria-label="Toggle menu"
            >
              <Menu className="w-6 h-6" style={{ color: "currentColor" }} />
            </button>
          </div>
        </div>

        {/* Mobile Menu */}
        <div className={`nav-menu-mobile ${isMenuOpen ? "active" : ""}`}>
          {showSearch && (
            <div className="search has-icon mb-6">
              <Search className="icon-search w-4 h-4" />
              <input
                type="text"
                placeholder="Search..."
                value={searchQuery}
                onChange={(e) => onSearchChange?.(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Escape") {
                    onClearSearch?.();
                  }
                }}
                className="input search-input w-full"
              />
              {searchQuery && (
                <button onClick={onClearSearch} className="search-clear">
                  <X className="w-4 h-4" />
                </button>
              )}
              {renderInlineSearch && renderInlineSearch()}
            </div>
          )}

          <ul className="nav-list vertical">
            <li>
              <button
                onClick={() => {
                  onInscribe?.();
                  setIsMenuOpen(false);
                }}
                className="nav-link"
              >
                Inscribe
              </button>
            </li>
            <li>
              <button
                onClick={() => {
                  navigate("/");
                  setIsMenuOpen(false);
                }}
                className="nav-link"
              >
                Blocks
              </button>
            </li>
            <li>
              <button
                onClick={() => {
                  navigate("/contracts");
                  setIsMenuOpen(false);
                }}
                className="nav-link"
              >
                Contracts
              </button>
            </li>
            <li>
              <button
                onClick={() => {
                  navigate("/discover");
                  setIsMenuOpen(false);
                }}
                className="nav-link"
              >
                Discover
              </button>
            </li>
            <li>
              <button
                onClick={() => {
                  navigate("/docs");
                  setIsMenuOpen(false);
                }}
                className="nav-link"
              >
                Documents
              </button>
            </li>
            {showBrcToggle && (
              <li>
                <button
                  onClick={() => {
                    onToggleBrc20?.();
                    setIsMenuOpen(false);
                  }}
                  className="nav-link flex flex-row items-center justify-between"
                >
                  <span>Hide BRC-20</span>
                  {hideBrc20 && <Check className="w-4 h-4 text-primary" />}
                </button>
              </li>
            )}
          </ul>

          <div className="mt-6 pt-6 border-t border-white/10 flex flex-col gap-4">
            {auth?.apiKey && (
              <div className="flex justify-center">
                <div className="badge badge-success text-xs px-3 py-1 rounded-full truncate max-w-full">
                  {auth.wallet || auth.email || "Connected"}
                </div>
              </div>
            )}

            <div className="flex flex-row items-center justify-between">
              {auth?.apiKey ? (
                <button
                  onClick={() => {
                    signOut();
                    setIsMenuOpen(false);
                  }}
                  className="nav-link text-error p-2 bg-transparent border-none"
                  title="Sign Out"
                >
                  <LogOut className="w-5 h-5" />
                </button>
              ) : (
                <button
                  onClick={() => {
                    navigate("/auth");
                    setIsMenuOpen(false);
                  }}
                  className="btn-starlight btn-sm"
                >
                  Sign In
                </button>
              )}

              {showThemeToggle && (
                <button
                  onClick={cycleTheme}
                  className="nav-link p-2 bg-transparent border-none"
                  title={getThemeTitle()}
                >
                  {getThemeIcon()}
                </button>
              )}
            </div>
          </div>
        </div>
      </nav>
    </header>
  );
};

export default AppHeader;
