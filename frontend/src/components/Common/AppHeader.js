import React from 'react';
import { Search, X, Moon, Sun } from 'lucide-react';
import { useNavigate } from 'react-router-dom';
import { useAuth } from '../../context/AuthContext';

const AppHeader = ({
  onInscribe,
  showSearch = false,
  searchQuery = '',
  onSearchChange,
  onClearSearch,
  renderInlineSearch,
  showBrcToggle = false,
  hideBrc20 = true,
  onToggleBrc20,
  showThemeToggle = false,
  isDarkMode = false,
  onToggleTheme
}) => {
  const navigate = useNavigate();
  const { auth, signOut } = useAuth();

  return (
    <header className="bg-gray-100 dark:bg-gray-900 border-b border-gray-300 dark:border-gray-800">
      <div className="container mx-auto px-6 py-4">
        <div className="flex items-center justify-between gap-4">
          <div className="flex items-center gap-8">
            <button
              onClick={() => navigate('/')}
              className="flex items-center gap-3 bg-transparent border-none cursor-pointer"
            >
              <div className="flex items-center justify-center w-8 h-8 bg-gradient-to-br from-indigo-500 to-purple-600 rounded-lg">
                <span className="text-white text-lg">✦</span>
              </div>
              <h1 className="text-2xl font-bold text-black dark:text-white">Starlight</h1>
            </button>

            <nav className="flex gap-6 text-sm">
              <button
                onClick={onInscribe}
                className="text-indigo-600 dark:text-indigo-400 hover:text-black dark:hover:text-white bg-transparent border-none cursor-pointer"
              >
                Inscribe
              </button>
              <button
                onClick={() => navigate('/')}
                className="text-gray-600 dark:text-gray-400 hover:text-black dark:hover:text-white bg-transparent border-none cursor-pointer"
              >
                Blocks
              </button>
              <button
                onClick={() => navigate('/contracts')}
                className="text-gray-600 dark:text-gray-400 hover:text-black dark:hover:text-white bg-transparent border-none cursor-pointer"
              >
                Contracts
              </button>
              <button
                onClick={() => navigate('/discover')}
                className="text-gray-600 dark:text-gray-400 hover:text-black dark:hover:text-white bg-transparent border-none cursor-pointer"
              >
                Discover
              </button>
              {showBrcToggle && (
                <button
                  onClick={onToggleBrc20}
                  className={`text-sm px-3 py-1 rounded-full border ${hideBrc20 ? 'border-indigo-500 text-indigo-600 dark:text-indigo-300' : 'border-gray-400 text-gray-600 dark:text-gray-300'} bg-transparent cursor-pointer`}
                  title="Toggle BRC-20 visibility"
                >
                  {hideBrc20 ? 'Hide BRC-20' : 'Show BRC-20'}
                </button>
              )}
            </nav>
          </div>

          <div className="flex items-center gap-4">
            {showSearch && (
              <div className="relative">
                <Search className="absolute left-3 top-1/2 transform -translate-y-1/2 w-4 h-4 text-gray-400 dark:text-gray-500" />
                <input
                  type="text"
                  placeholder="Search inscriptions..."
                  value={searchQuery}
                  onChange={(e) => onSearchChange?.(e.target.value)}
                  className="bg-gray-200 dark:bg-gray-800 text-black dark:text-white pl-10 pr-10 py-2 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500 w-64"
                />
                {searchQuery && (
                  <button
                    onClick={onClearSearch}
                    className="absolute right-3 top-1/2 transform -translate-y-1/2 text-gray-400 dark:text-gray-500 hover:text-gray-600 dark:hover:text-gray-300"
                  >
                    <X className="w-4 h-4" />
                  </button>
                )}
                {renderInlineSearch && renderInlineSearch()}
              </div>
            )}
            {auth?.apiKey ? (
              <div className="flex items-center gap-2">
                <div className="px-3 py-1 rounded-full bg-emerald-600 text-white text-sm">
                  {auth.wallet || auth.email || `Key …${auth.apiKey.slice(-6)}`}
                </div>
                <button
                  onClick={signOut}
                  className="text-xs text-gray-500 dark:text-gray-400 hover:text-black dark:hover:text-white"
                >
                  Sign out
                </button>
              </div>
            ) : (
              <button
                onClick={() => navigate('/auth')}
                className="text-sm px-3 py-1 rounded-full border border-gray-300 dark:border-gray-700 text-gray-600 dark:text-gray-300 hover:text-black dark:hover:text-white"
              >
                Sign In
              </button>
            )}
            {showThemeToggle && (
              <button
                onClick={onToggleTheme}
                className="text-gray-600 dark:text-gray-400 hover:text-black dark:hover:text-white"
              >
                {isDarkMode ? <Moon className="w-5 h-5" /> : <Sun className="w-5 h-5" />}
              </button>
            )}
          </div>
        </div>
      </div>
    </header>
  );
};

export default AppHeader;
