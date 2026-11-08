/**
 * Starlight UI - Interactive Helpers
 * Standardized components for the QuantumCSS framework
 */

/**
 * Resolve localStorage key for theme preference.
 * - data-theme-storage="path"|"pathname" → per-path key (avoids multi-demo pollution)
 * - data-theme-storage="<custom>" → that exact key
 * - default → "theme"
 * @param {HTMLElement} [html]
 * @returns {string}
 */
function resolveThemeStorageKey(html) {
  const el = html || (typeof document !== 'undefined' ? document.documentElement : null);
  if (!el) return 'theme';
  const attr = el.getAttribute('data-theme-storage');
  if (!attr) return 'theme';
  if (attr === 'path' || attr === 'pathname') {
    try {
      return `theme:${typeof location !== 'undefined' ? location.pathname : ''}`;
    } catch {
      return 'theme';
    }
  }
  return attr;
}

/**
 * Apply theme before first paint to avoid FOUC.
 * Call from an inline <script> in <head>, or rely on DOMContentLoaded init.
 *
 * Honors (in order):
 * 1. data-theme-locked → always data-theme-default (ignore storage)
 * 2. localStorage[storageKey] when set
 * 3. html[data-theme-default]
 * 4. "dark"
 *
 * @param {string} [storageKey] - Override; otherwise resolveThemeStorageKey()
 */
function applyThemeBootstrap(storageKey) {
  if (typeof document === 'undefined') return 'dark';
  const html = document.documentElement;
  const defaultTheme = html.getAttribute('data-theme-default') || 'dark';
  const key = storageKey || resolveThemeStorageKey(html);
  const locked = html.hasAttribute('data-theme-locked');

  let theme = defaultTheme;
  if (!locked) {
    try {
      const saved = localStorage.getItem(key);
      if (saved) theme = saved;
    } catch { /* private mode */ }
  }

  let effective = theme;
  if (theme === 'auto') {
    effective = window.matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark';
  } else if (theme !== 'light' && theme !== 'dark') {
    effective = defaultTheme === 'light' ? 'light' : 'dark';
  }
  html.setAttribute('data-theme', effective);
  return effective;
}

/**
 * Tag <html data-os="macos|windows|linux"> so CSS can tune tracking,
 * smoothing, and native font stacks. Apps may set data-os themselves
 * or lock it with data-os-locked (screenshot / theme previews).
 *
 * @param {HTMLElement} [html]
 * @returns {string}
 */
function applyOsBootstrap(html) {
  if (typeof document === 'undefined') return 'other';
  const el = html || document.documentElement;
  if (el.hasAttribute('data-os-locked')) {
    return el.getAttribute('data-os') || 'other';
  }
  if (el.hasAttribute('data-os')) {
    return el.getAttribute('data-os');
  }

  const ua = (typeof navigator !== 'undefined' && navigator.userAgent) || '';
  const platform =
    (typeof navigator !== 'undefined' &&
      ((navigator.userAgentData && navigator.userAgentData.platform) ||
        navigator.platform)) ||
    '';
  const hay = `${platform} ${ua}`;

  let os = 'other';
  if (/iPhone|iPad|iPod|Macintosh|Mac OS X|MacIntel/.test(hay)) {
    os = 'macos';
  } else if (/Win32|Win64|Windows/.test(hay)) {
    os = 'windows';
  } else if (/Linux|X11|CrOS|Android/.test(hay)) {
    os = 'linux';
  }

  el.setAttribute('data-os', os);
  return os;
}

// Run immediately when this file is loaded (still helps if placed in <head>)
if (typeof document !== 'undefined') {
  applyThemeBootstrap();
  applyOsBootstrap();
}

const Starlight = {
  applyThemeBootstrap,
  applyOsBootstrap,
  resolveThemeStorageKey,
  /**
   * Initializes a randomized star field in the target container.
   * @param {string} selector - CSS selector for the container (default: '.starlight-stars')
   * @param {number} count - Number of stars to generate (default: 100)
   */
  initStars(selector = '.starlight-stars', count = 100) {
    const containers = document.querySelectorAll(selector);
    
    // Check for reduced motion preference
    const prefersReducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
    
    containers.forEach(container => {
      // Clear existing stars if any
      container.innerHTML = '';
      
      // If user prefers reduced motion, maybe skip or show static stars
      if (prefersReducedMotion) {
        count = Math.floor(count / 2); // Show fewer stars if motion is reduced
      }
      
      for (let i = 0; i < count; i++) {
        const star = document.createElement('div');
        star.className = 'star';
        
        // Randomize position
        star.style.left = `${Math.random() * 100}%`;
        star.style.top = `${Math.random() * 100}%`;
        
        // Randomize size (1px to 3px)
        const size = Math.random() * 2 + 1;
        star.style.width = `${size}px`;
        star.style.height = `${size}px`;
        
        // Randomize animation duration (2s to 5s)
        if (!prefersReducedMotion) {
          star.style.setProperty('--q-duration', `${Math.random() * 3 + 2}s`);
        } else {
          star.style.animation = 'none';
          star.style.opacity = (Math.random() * 0.3 + 0.1).toString();
        }
        
        container.appendChild(star);
      }
    });
  },

  /**
   * Initializes navigation menu toggles for mobile view.
   * Uses data attributes for flexible targeting and reduced DOM dependencies.
   */
  initNavigation() {
    const toggles = document.querySelectorAll('[data-nav-toggle]');
    
    toggles.forEach(toggle => {
      const config = {
        targetSelector: toggle.getAttribute('data-nav-target'),
        targetId: toggle.getAttribute('data-nav-target-id'),
        activeClass: toggle.getAttribute('data-nav-active-class') || 'active',
        closeOnOutside: toggle.getAttribute('data-nav-close-outside') !== 'false'
      };
      
      // Find target menu with multiple fallback methods
      let menu = null;
      
      if (config.targetId) {
        menu = document.getElementById(config.targetId);
      } else if (config.targetSelector) {
        menu = document.querySelector(config.targetSelector);
      } else {
        // Fallback: look for common patterns
        const nav = toggle.closest('nav') || toggle.closest('[data-nav]') || toggle.parentElement;
        if (nav) {
          menu = nav.querySelector('.nav-menu-mobile, [data-nav-menu]');
          if (!menu && nav.nextElementSibling) {
            menu = nav.nextElementSibling.querySelector('.nav-menu-mobile, [data-nav-menu]') || 
                   nav.nextElementSibling;
          }
        }
      }
      
      if (menu) {
        toggle.addEventListener('click', (e) => {
          e.preventDefault();
          e.stopPropagation();
          
          const isActive = toggle.classList.toggle(config.activeClass);
          menu.classList.toggle(config.activeClass, isActive);
          
          // Update aria attributes for accessibility
          toggle.setAttribute('aria-expanded', isActive);
          menu.setAttribute('aria-hidden', !isActive);
          
          // Emit custom event
          toggle.dispatchEvent(new CustomEvent('navToggle', {
            detail: { menu, isActive }
          }));
        });

        // Close menu when clicking outside (optional)
        if (config.closeOnOutside) {
          const handleOutsideClick = (e) => {
            if (!menu.contains(e.target) && !toggle.contains(e.target)) {
              menu.classList.remove(config.activeClass);
              toggle.classList.remove(config.activeClass);
              toggle.setAttribute('aria-expanded', 'false');
              menu.setAttribute('aria-hidden', 'true');
            }
          };
          
          document.addEventListener('click', handleOutsideClick);
          
          // Clean up on component destruction
          toggle.addEventListener('navDestroy', () => {
            document.removeEventListener('click', handleOutsideClick);
          });
        }
      }
    });
    
    // Fallback for legacy hamburger (backward compatibility)
    const legacyToggles = document.querySelectorAll('.hamburger:not([data-nav-toggle])');
    legacyToggles.forEach(toggle => {
      const nav = toggle.closest('nav') || toggle.closest('.nav-header') || toggle.closest('.nav-glass') || toggle.parentElement;
      if (!nav) return;

      let menu = nav.querySelector('.nav-menu-mobile');
      if (!menu && nav.nextElementSibling && nav.nextElementSibling.classList.contains('nav-menu-mobile')) {
        menu = nav.nextElementSibling;
      }
      
      if (menu) {
        toggle.addEventListener('click', (e) => {
          e.stopPropagation();
          const isActive = toggle.classList.toggle('active');
          menu.classList.toggle('active', isActive);
          
          // Allow nav to wrap when menu is open so full-width menu stacks below
          const parentNav = menu.closest('.nav-header') || menu.closest('.nav-glass');
          if (parentNav) {
            parentNav.style.flexFlow = isActive ? 'row wrap' : 'nowrap';
          }
        });

        document.addEventListener('click', (e) => {
          if (!menu.contains(e.target) && !toggle.contains(e.target)) {
            menu.classList.remove('active');
            toggle.classList.remove('active');
          }
        });
      }
    });
  },

  /**
   * Initializes dropdown menus.
   * Toggles '.active' class on '.dropdown' elements when clicked.
   */
  initDropdowns() {
    const dropdowns = document.querySelectorAll('.dropdown');
    
    dropdowns.forEach(dropdown => {
      const toggle = dropdown.querySelector('.dropdown-toggle') || dropdown.querySelector('button') || dropdown.querySelector('a');
      
      if (toggle) {
        toggle.addEventListener('click', (e) => {
          // If it's a link that points somewhere, let it work normally
          if (toggle.tagName === 'A' && toggle.getAttribute('href') && toggle.getAttribute('href') !== '#') {
            return;
          }
          
          e.preventDefault();
          e.stopPropagation();
          
          const isActive = dropdown.classList.contains('active');
          
          // Close all other dropdowns
          document.querySelectorAll('.dropdown.active').forEach(d => {
            if (d !== dropdown) d.classList.remove('active');
          });
          
          dropdown.classList.toggle('active', !isActive);
        });
      }
    });

    // Close dropdowns when clicking outside
    document.addEventListener('click', (e) => {
      if (!e.target.closest('.dropdown')) {
        document.querySelectorAll('.dropdown.active').forEach(d => {
          d.classList.remove('active');
        });
      }
    });
  },

  /**
   * Initializes accordion components.
   * Uses data attributes for flexible configuration and reduced DOM dependencies.
   */
  initAccordions() {
    const accordions = document.querySelectorAll('[data-accordion]');
    
    accordions.forEach(accordion => {
      const config = {
        allowMultiple: accordion.getAttribute('data-accordion-allow-multiple') === 'true',
        closeOthers: accordion.getAttribute('data-accordion-close-others') !== 'false',
        headerSelector: accordion.getAttribute('data-accordion-header') || '.accordion-header',
        contentSelector: accordion.getAttribute('data-accordion-content') || '.accordion-content',
        itemSelector: accordion.getAttribute('data-accordion-item') || '.accordion-item'
      };
      
      // Find headers within this accordion
      const headers = accordion.querySelectorAll(config.headerSelector);
      
      headers.forEach(header => {
        header.addEventListener('click', (e) => {
          e.preventDefault();
          
          // Find the associated item (could be parent, sibling, or specified via data attribute)
          let item = header.closest(config.itemSelector);
          if (!item) {
            const itemId = header.getAttribute('data-accordion-item-id');
            if (itemId) {
              item = document.getElementById(itemId);
            }
          }
          
          if (!item) return;
          
          const isActive = item.classList.contains('active');
          const groupId = accordion.getAttribute('data-accordion-group');
          
          // Handle accordion grouping logic
          if (config.closeOthers && !config.allowMultiple && !isActive) {
            if (groupId) {
              // Close items in same group across different accordions
              document.querySelectorAll(`[data-accordion-group="${groupId}"] ${config.itemSelector}.active`)
                .forEach(i => i.classList.remove('active'));
            } else {
              // Close items in this accordion only
              accordion.querySelectorAll(`${config.itemSelector}.active`)
                .forEach(i => i.classList.remove('active'));
            }
          }
          
          // Toggle current item
          item.classList.toggle('active', !isActive);
          
          // Emit custom event for external handling
          accordion.dispatchEvent(new CustomEvent('accordionToggle', {
            detail: { item, isActive: !isActive }
          }));
        });
      });
    });
    
    // Fallback for legacy accordion-header (backward compatibility)
    // Only target headers that are NOT already managed by a data-accordion container
    const legacyHeaders = document.querySelectorAll('.accordion-header:not([data-accordion])');
    legacyHeaders.forEach(header => {
      if (header.closest('[data-accordion]')) return;
      
      const item = header.parentElement;
      const group = item.closest('.accordion-group');
      
      header.addEventListener('click', () => {
        const isActive = item.classList.contains('active');
        
        // If in a group, close others
        if (group) {
          group.querySelectorAll('.accordion-item').forEach(i => i.classList.remove('active'));
        }
        
        item.classList.toggle('active', !isActive);
      });
    });
  },

  /**
   * Initializes tab components.
   * Uses data attributes for flexible configuration and reduced DOM dependencies.
   */
  initTabs() {
    const tabContainers = document.querySelectorAll('[data-tabs]');
    
    tabContainers.forEach(container => {
      const config = {
        buttonSelector: container.getAttribute('data-tabs-buttons') || '.tab-button',
        panelSelector: container.getAttribute('data-tabs-panels') || '.tab-panel',
        activeClass: container.getAttribute('data-tabs-active-class') || 'active',
        orientation: container.getAttribute('data-tabs-orientation') || 'horizontal',
        initialTab: container.getAttribute('data-tabs-initial')
      };
      
      // Find buttons and panels with flexible targeting
      let buttons, panels;
      
      if (container.hasAttribute('data-tabs-buttons')) {
        buttons = document.querySelectorAll(config.buttonSelector);
      } else {
        buttons = container.querySelectorAll(config.buttonSelector);
      }
      
      if (container.hasAttribute('data-tabs-panels')) {
        panels = document.querySelectorAll(config.panelSelector);
      } else {
        panels = container.querySelectorAll(config.panelSelector);
      }
      
      // Initialize active tab
      let activeTabId = config.initialTab;
      if (!activeTabId) {
        const firstButton = buttons[0];
        if (firstButton) {
          activeTabId = firstButton.getAttribute('data-tab') || firstButton.getAttribute('data-tabs-target');
        }
      }
      
      // Set initial state
      if (activeTabId) {
        buttons.forEach(btn => {
          const btnTargetId = btn.getAttribute('data-tab') || btn.getAttribute('data-tabs-target');
          btn.classList.toggle(config.activeClass, btnTargetId === activeTabId);
          btn.setAttribute('aria-selected', btnTargetId === activeTabId);
          btn.setAttribute('tabindex', btnTargetId === activeTabId ? '0' : '-1');
        });
        
        panels.forEach(panel => {
          panel.classList.toggle(config.activeClass, panel.id === activeTabId);
          panel.setAttribute('aria-hidden', panel.id !== activeTabId);
        });
      }
      
      // Add click handlers
      buttons.forEach(button => {
        button.addEventListener('click', (e) => {
          e.preventDefault();
          
          const targetId = button.getAttribute('data-tab') || button.getAttribute('data-tabs-target');
          if (!targetId) return;
          
          // Update buttons
          buttons.forEach(btn => {
            const btnTargetId = btn.getAttribute('data-tab') || btn.getAttribute('data-tabs-target');
            const isActive = btnTargetId === targetId;
            btn.classList.toggle(config.activeClass, isActive);
            btn.setAttribute('aria-selected', isActive);
            btn.setAttribute('tabindex', isActive ? '0' : '-1');
          });
          
          // Update panels
          panels.forEach(panel => {
            const isActive = panel.id === targetId;
            panel.classList.toggle(config.activeClass, isActive);
            panel.setAttribute('aria-hidden', !isActive);
          });
          
          // Emit custom event
          container.dispatchEvent(new CustomEvent('tabChange', {
            detail: { targetId, activeButton: button, activePanel: document.getElementById(targetId) }
          }));
        });
        
        // Keyboard navigation
        button.addEventListener('keydown', (e) => {
          const currentIndex = Array.from(buttons).indexOf(button);
          let nextIndex;
          
          switch (e.key) {
            case 'ArrowLeft':
            case 'ArrowUp':
              e.preventDefault();
              nextIndex = currentIndex > 0 ? currentIndex - 1 : buttons.length - 1;
              break;
            case 'ArrowRight':
            case 'ArrowDown':
              e.preventDefault();
              nextIndex = currentIndex < buttons.length - 1 ? currentIndex + 1 : 0;
              break;
            case 'Home':
              e.preventDefault();
              nextIndex = 0;
              break;
            case 'End':
              e.preventDefault();
              nextIndex = buttons.length - 1;
              break;
            default:
              return;
          }
          
          if (nextIndex !== undefined) {
            buttons[nextIndex].focus();
            buttons[nextIndex].click();
          }
        });
      });
    });
    
    // Fallback for legacy tab-list (backward compatibility)
    const legacyTabLists = document.querySelectorAll('.tab-list:not([data-tabs])');
    legacyTabLists.forEach(list => {
      const buttons = list.querySelectorAll('.tab-button');
      const container = list.parentElement;
      
      buttons.forEach(button => {
        button.addEventListener('click', () => {
          const targetId = button.getAttribute('data-tab');
          if (!targetId) return;
          
          buttons.forEach(btn => btn.classList.remove('active'));
          button.classList.add('active');
          
          const panels = container.querySelectorAll('.tab-panel');
          panels.forEach(panel => {
            panel.classList.toggle('active', panel.id === targetId);
          });
        });
      });
    });
  },

  /**
   * Creates a desktop-style window manager for draggable, resizable surfaces.
   * Windows use shared Starlight classes and can opt into custom behavior
   * via hooks or per-window metadata.
   */
  createWindowManager(options = {}) {
    const config = {
      container: options.container || null,
      windowSelector: options.windowSelector || '.window',
      headerSelector: options.headerSelector || '.window-header',
      resizeSelector: options.resizeSelector || '[data-window-resize], .window-resize',
      closeSelector: options.closeSelector || '[data-window-close]',
      minimizeSelector: options.minimizeSelector || '[data-window-minimize]',
      maximizeSelector: options.maximizeSelector || '[data-window-maximize]',
      focusedClass: options.focusedClass || 'focused',
      minimizedClass: options.minimizedClass || 'minimized',
      maximizedClass: options.maximizedClass || 'maximized',
      draggingClass: options.draggingClass || 'dragging',
      floatingAttr: options.floatingAttr || 'data-floating',
      mobileBreakpoint: options.mobileBreakpoint || '(max-width: 860px)',
      initialZ: typeof options.initialZ === 'number' ? options.initialZ : 20,
      onFocus: typeof options.onFocus === 'function' ? options.onFocus : null,
      onClose: typeof options.onClose === 'function' ? options.onClose : null,
      onMinimize: typeof options.onMinimize === 'function' ? options.onMinimize : null,
      onMaximize: typeof options.onMaximize === 'function' ? options.onMaximize : null
    };

    const container = typeof config.container === 'string'
      ? document.querySelector(config.container)
      : config.container;

    if (!container) {
      console.warn('Starlight: createWindowManager requires a valid container.');
      return null;
    }

    let topZ = config.initialZ;
    let createdWindowCount = 0;
    const managedWindows = new Map();
    let dragState = null;
    let resizeState = null;

    const isMobile = () => window.matchMedia(config.mobileBreakpoint).matches;

    const resolveHookResult = (hook, payload) => {
      if (typeof hook !== 'function') return false;
      return hook(payload) === true;
    };

    const focusWindow = (windowEl, trigger = null) => {
      managedWindows.forEach((meta, element) => {
        element.classList.toggle(config.focusedClass, element === windowEl);
      });

      windowEl.classList.remove(config.minimizedClass);
      windowEl.style.zIndex = String(++topZ);

      const meta = managedWindows.get(windowEl) || {};
      if (meta.onFocus) {
        meta.onFocus({ windowEl, meta, trigger, api });
      }
      if (config.onFocus) {
        config.onFocus({ windowEl, meta, trigger, api });
      }
    };

    const pinWindowToCurrentRect = (windowEl) => {
      if (isMobile()) return;
      if (windowEl.getAttribute(config.floatingAttr) === 'true') return;

      const hostRect = container.getBoundingClientRect();
      const rect = windowEl.getBoundingClientRect();

      windowEl.style.inset = 'auto';
      windowEl.style.left = `${rect.left - hostRect.left}px`;
      windowEl.style.top = `${rect.top - hostRect.top}px`;
      windowEl.style.width = `${rect.width}px`;
      windowEl.style.height = `${rect.height}px`;
      windowEl.style.right = 'auto';
      windowEl.style.bottom = 'auto';
      windowEl.setAttribute(config.floatingAttr, 'true');
    };

    const releaseWindowToFlow = (windowEl) => {
      windowEl.style.left = '';
      windowEl.style.top = '';
      windowEl.style.width = '';
      windowEl.style.height = '';
      windowEl.style.right = '';
      windowEl.style.bottom = '';
      windowEl.style.inset = '';
      windowEl.setAttribute(config.floatingAttr, 'false');
    };

    const unregisterWindow = (windowEl) => {
      managedWindows.delete(windowEl);
      windowEl.remove();
    };

    const registerWindow = (windowEl, meta = {}) => {
      if (!windowEl || managedWindows.has(windowEl)) return windowEl;

      managedWindows.set(windowEl, meta);
      windowEl.style.zIndex = String(++topZ);

      const header = windowEl.querySelector(config.headerSelector);
      const resizeHandle = windowEl.querySelector(config.resizeSelector);
      const closeButton = windowEl.querySelector(config.closeSelector);
      const minButton = windowEl.querySelector(config.minimizeSelector);
      const maxButton = windowEl.querySelector(config.maximizeSelector);

      windowEl.addEventListener('mousedown', (event) => {
        if (event.target.closest(config.resizeSelector)) return;
        focusWindow(windowEl);
      });

      if (header) {
        header.addEventListener('pointerdown', (event) => {
          if (isMobile()) return;
          if (event.target.closest(config.closeSelector)
            || event.target.closest(config.minimizeSelector)
            || event.target.closest(config.maximizeSelector)
            || event.target.closest('[data-window-toolbar]')) {
            return;
          }
          if (windowEl.classList.contains(config.maximizedClass)) return;

          const hasPosition = windowEl.style.left || windowEl.style.top;
          if (!hasPosition) {
            pinWindowToCurrentRect(windowEl);
          }
          const hostRect = container.getBoundingClientRect();
          const rect = windowEl.getBoundingClientRect();

          focusWindow(windowEl);
          header.setPointerCapture(event.pointerId);
          dragState = {
            windowEl,
            pointerId: event.pointerId,
            hostRect,
            offsetX: event.clientX - rect.left,
            offsetY: event.clientY - rect.top
          };
          windowEl.classList.add(config.draggingClass);
        });
      }

      if (resizeHandle) {
        resizeHandle.addEventListener('pointerdown', (event) => {
          if (isMobile()) return;
          if (windowEl.classList.contains(config.maximizedClass)) return;

          event.stopPropagation();
          const hasPosition = windowEl.style.left || windowEl.style.top;
          if (!hasPosition) {
            pinWindowToCurrentRect(windowEl);
          }
          const rect = windowEl.getBoundingClientRect();

          resizeHandle.setPointerCapture(event.pointerId);
          resizeState = {
            windowEl,
            pointerId: event.pointerId,
            startX: event.clientX,
            startY: event.clientY,
            width: rect.width,
            height: rect.height
          };
        });
      }

      if (closeButton) {
        closeButton.addEventListener('click', (event) => {
          event.stopPropagation();
          const payload = { windowEl, meta, api };
          if (resolveHookResult(meta.onClose, payload) || resolveHookResult(config.onClose, payload)) {
            return;
          }
          windowEl.classList.add(config.minimizedClass);
        });
      }

      if (minButton) {
        minButton.addEventListener('click', (event) => {
          event.stopPropagation();
          const payload = { windowEl, meta, api };
          if (resolveHookResult(meta.onMinimize, payload) || resolveHookResult(config.onMinimize, payload)) {
            return;
          }
          windowEl.classList.add(config.minimizedClass);
        });
      }

      if (maxButton) {
        maxButton.addEventListener('click', (event) => {
          event.stopPropagation();
          const payload = { windowEl, meta, api };
          if (resolveHookResult(meta.onMaximize, payload) || resolveHookResult(config.onMaximize, payload)) {
            return;
          }

          if (!windowEl.classList.contains(config.maximizedClass)) {
            windowEl.dataset.prevLeft = windowEl.style.left || '';
            windowEl.dataset.prevTop = windowEl.style.top || '';
            windowEl.dataset.prevWidth = windowEl.style.width || '';
            windowEl.dataset.prevHeight = windowEl.style.height || '';
            windowEl.classList.add(config.maximizedClass);
            windowEl.style.left = '0';
            windowEl.style.top = '0';
          } else {
            windowEl.classList.remove(config.maximizedClass);
            windowEl.style.left = windowEl.dataset.prevLeft;
            windowEl.style.top = windowEl.dataset.prevTop;
            windowEl.style.width = windowEl.dataset.prevWidth;
            windowEl.style.height = windowEl.dataset.prevHeight;
          }

          focusWindow(windowEl);
        });
      }

      return windowEl;
    };

    const createWindow = ({
      windowId,
      title = '',
      subtitle = '',
      bodyHTML = '',
      toolbarHTML = '',
      width = 640,
      height = 420,
      left,
      top,
      className = '',
      meta = {}
    }) => {
      createdWindowCount += 1;
      const resolvedLeft = typeof left === 'number'
        ? left
        : 120 + ((createdWindowCount - 1) % 4) * 32;
      const resolvedTop = typeof top === 'number'
        ? top
        : 80 + ((createdWindowCount - 1) % 4) * 24;
      const windowEl = document.createElement('section');
      windowEl.className = `window ${className}`.trim();
      if (windowId) {
        windowEl.dataset.window = windowId;
      }
      windowEl.setAttribute(config.floatingAttr, 'true');
      windowEl.style.left = `${resolvedLeft}px`;
      windowEl.style.top = `${resolvedTop}px`;
      windowEl.style.width = `${width}px`;
      windowEl.style.height = `${height}px`;
      windowEl.style.right = 'auto';
      windowEl.style.bottom = 'auto';
      windowEl.style.inset = 'auto';
      windowEl.innerHTML = `
        <header class="window-header">
          <div class="window-title">
            <div class="window-traffic">
              <button class="window-control is-close" type="button" data-window-close aria-label="Close"></button>
              <button class="window-control is-minimize" type="button" data-window-minimize aria-label="Minimize"></button>
              <button class="window-control is-maximize" type="button" data-window-maximize aria-label="Expand"></button>
            </div>
            <div>
              <strong>${title}</strong>
              <span>${subtitle}</span>
            </div>
          </div>
          <div class="window-toolbar" data-window-toolbar>${toolbarHTML}</div>
        </header>
        <div class="window-body">${bodyHTML}</div>
        <div class="window-resize" data-window-resize></div>`;

      container.appendChild(windowEl);
      registerWindow(windowEl, meta);
      focusWindow(windowEl);
      return windowEl;
    };

    document.addEventListener('pointermove', (event) => {
      if (dragState) {
        const maxLeft = dragState.hostRect.width - 260;
        const maxTop = dragState.hostRect.height - 140;
        const nextLeft = event.clientX - dragState.hostRect.left - dragState.offsetX;
        const nextTop = event.clientY - dragState.hostRect.top - dragState.offsetY;
        dragState.windowEl.style.left = `${Math.max(0, Math.min(maxLeft, nextLeft))}px`;
        dragState.windowEl.style.top = `${Math.max(0, Math.min(maxTop, nextTop))}px`;
      }

      if (resizeState) {
        const nextWidth = Math.max(360, resizeState.width + (event.clientX - resizeState.startX));
        const nextHeight = Math.max(240, resizeState.height + (event.clientY - resizeState.startY));
        resizeState.windowEl.style.width = `${nextWidth}px`;
        resizeState.windowEl.style.height = `${nextHeight}px`;
      }
    });

    document.addEventListener('pointerup', () => {
      if (dragState) {
        dragState.windowEl.classList.remove(config.draggingClass);
      }
      dragState = null;
      resizeState = null;
    });

    const api = {
      config,
      container,
      registerWindow,
      unregisterWindow,
      focusWindow,
      pinWindowToCurrentRect,
      createWindow,
      getMeta(windowEl) {
        return managedWindows.get(windowEl);
      },
      getWindow(windowId) {
        return container.querySelector(`${config.windowSelector}[data-window="${windowId}"]`);
      },

      initializeStaticWindows(metaResolver = null) {
        container.querySelectorAll(config.windowSelector).forEach((windowEl) => {
          if (managedWindows.has(windowEl)) return;
          if (isMobile()) {
            releaseWindowToFlow(windowEl);
          }
          const meta = typeof metaResolver === 'function' ? metaResolver(windowEl) : {};
          registerWindow(windowEl, meta || {});
        });
      }
    };

    return api;
  },

  /**
   * Initializes theme management using data attributes for AI predictability.
   * Manages theme state via localStorage and html[data-theme] attribute.
   * Configurable via data attributes on theme toggle elements.
   */
  initTheme() {
    const htmlEl = document.documentElement;
    const config = {
      defaultTheme: htmlEl.getAttribute('data-theme-default') || 'dark',
      storageKey: resolveThemeStorageKey(htmlEl),
      themes: ['light', 'dark', 'auto'],
      iconSelector: {
        light: '.sun-icon, [data-theme-icon="light"]',
        dark: '.moon-icon, [data-theme-icon="dark"]',
        // `.icon-display` is the monitor glyph. Content (gallery cards, feature
        // icons) uses it too, so auto-mode visibility is `.auto-icon` only.
        // Bare monitor icons beside sun/moon stay supported for older toggles.
        auto: '.auto-icon, [data-theme-icon="auto"]'
      },
      autoDetect: true,
      locked: htmlEl.hasAttribute('data-theme-locked')
    };
    // Override config with global settings if available
    if (window.StarlightConfig && window.StarlightConfig.theme) {
      Object.assign(config, window.StarlightConfig.theme);
    }
    // Re-apply after config merge (storage key / locked may change)
    applyThemeBootstrap(config.storageKey);

    const html = document.documentElement;

    /**
     * Updates the UI icons to match the target theme
     * @param {string} theme - The theme that is active (light, dark, or auto)
     * @param {string} effectiveTheme - The actual theme being displayed
     */
    const themeIconNodes = (role) => {
      const selector = config.iconSelector[role];
      const nodes = selector ? [...document.querySelectorAll(selector)] : [];
      if (role !== 'auto') return nodes;

      const seen = new Set(nodes);
      document.querySelectorAll('.icon-display').forEach((icon) => {
        if (seen.has(icon)) return;
        const parent = icon.parentElement;
        if (!parent) return;
        const besideThemeIcon = parent.querySelector(
          ':scope > .sun-icon, :scope > .moon-icon, :scope > [data-theme-icon="light"], :scope > [data-theme-icon="dark"]'
        );
        if (!besideThemeIcon) return;
        seen.add(icon);
        nodes.push(icon);
      });
      return nodes;
    };

    const updateIcons = (theme, effectiveTheme) => {
      const autoNodes = themeIconNodes('auto');

      if (theme === 'auto' && autoNodes.length > 0) {
        autoNodes.forEach((icon) => {
          icon.classList.remove('hidden');
        });
        const lightSelector = config.iconSelector.light;
        const darkSelector = config.iconSelector.dark;
        const hideSelector = [lightSelector, darkSelector].filter(Boolean).join(', ');
        if (hideSelector) {
          document.querySelectorAll(hideSelector).forEach((icon) => {
            icon.classList.add('hidden');
          });
        }
        return;
      }

      config.themes.forEach((t) => {
        themeIconNodes(t).forEach((icon) => {
          const isEffective = theme === 'auto' ? t === effectiveTheme : t === theme;
          icon.classList.toggle('hidden', !isEffective);
        });
      });
    };

    /**
     * Sets the theme and updates storage/UI
     * @param {string} theme - The theme to set (light, dark, or auto)
     * @param {string} source - Source of the change for event detail
     * @param {boolean} save - Whether to save the preference to localStorage
     */
    window.setTheme = (theme, source = 'api', save = true) => {
      if (!config.themes.includes(theme)) {
        console.warn(`Starlight: Theme "${theme}" is not supported.`);
        return;
      }

      const previousTheme = localStorage.getItem(config.storageKey) || (config.autoDetect ? 'auto' : config.defaultTheme);
      let effectiveTheme = theme;

      if (theme === 'auto' && config.autoDetect) {
        effectiveTheme = window.matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark';
        localStorage.setItem(`${config.storageKey}-effective`, effectiveTheme);
      } else {
        localStorage.removeItem(`${config.storageKey}-effective`);
      }

      // Apply to DOM
      html.setAttribute('data-theme', effectiveTheme);
      
      // Save preference (skip when brand-locked unless caller forces save)
      if (save && !config.locked) {
        localStorage.setItem(config.storageKey, theme);
      }
      
      // Update icons
      updateIcons(theme, effectiveTheme);

      // Dispatch event
      window.dispatchEvent(new CustomEvent('themechange', {
        detail: { theme, effectiveTheme, previousTheme, source }
      }));

      return theme;
    };

    /**
     * Cycles to the next available theme
     */
    window.toggleTheme = () => {
      const currentTheme = localStorage.getItem(config.storageKey) || (config.autoDetect ? 'auto' : config.defaultTheme);
      const currentIndex = config.themes.indexOf(currentTheme);
      const nextIndex = (currentIndex + 1) % config.themes.length;
      const nextTheme = config.themes[nextIndex];
      
      return window.setTheme(nextTheme, 'toggle');
    };

    // Initialize individual toggles
    const createThemeToggle = (element) => {
      element.addEventListener('click', (e) => {
        e.preventDefault();
        window.toggleTheme();
      });
    };

    // Auto-detect system theme changes
    if (config.autoDetect) {
      window.matchMedia('(prefers-color-scheme: light)').addEventListener('change', (e) => {
        const savedTheme = localStorage.getItem(config.storageKey);
        const currentActiveTheme = savedTheme || 'auto';
        
        if (currentActiveTheme === 'auto') {
          const newEffective = e.matches ? 'light' : 'dark';
          
          // Apply to DOM
          html.setAttribute('data-theme', newEffective);
          
          localStorage.setItem(`${config.storageKey}-effective`, newEffective);
          updateIcons('auto', newEffective);
          
          window.dispatchEvent(new CustomEvent('themechange', {
            detail: { theme: 'auto', effectiveTheme: newEffective, source: 'system' }
          }));
        }
      });
    }

    // Initialize UI
    document.querySelectorAll('.theme-toggle, [data-theme-toggle]').forEach(createThemeToggle);

    // Set initial theme
    const savedTheme = localStorage.getItem(config.storageKey);
    if (savedTheme) {
      window.setTheme(savedTheme, 'init');
    } else {
      window.setTheme(config.autoDetect ? 'auto' : config.defaultTheme, 'init', false);
    }
  }
};

// Auto-initialize if the element exists and we're in a browser
if (typeof window !== 'undefined') {
  window.Starlight = Starlight;
  document.addEventListener('DOMContentLoaded', () => {
    if (document.querySelector('.starlight-stars')) {
      Starlight.initStars();
    }
    Starlight.initNavigation();
    Starlight.initDropdowns();
    Starlight.initAccordions();
    Starlight.initTabs();
    Starlight.initTheme();
  });
}
