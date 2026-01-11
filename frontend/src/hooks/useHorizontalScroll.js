import { useRef, useEffect, useState } from 'react';

export const useHorizontalScroll = () => {
  const elRef = useRef();
  const [isDragging, setIsDragging] = useState(false);
  const dragThreshold = 5; // Pixels to move before considering it a drag

  // Refs for animation and velocity
  const animationFrameId = useRef(null);
  const scrollVelocity = useRef(0); // Renamed from 'velocity' to avoid confusion with drag calculation
  const lastPosition = useRef(0); // To track position for velocity calculation during drag
  const lastFrameTime = useRef(0); // To track time for velocity calculation during drag

  // State for drag
  const isDown = useRef(false);
  const startX = useRef(0);
  const initialScrollLeft = useRef(0);
  const initialPageX = useRef(0);

  useEffect(() => {
    const el = elRef.current;
    if (el) {
      const stopAnimation = () => {
        if (animationFrameId.current) {
          cancelAnimationFrame(animationFrameId.current);
          animationFrameId.current = null;
          scrollVelocity.current = 0;
        }
      };

      const animateScroll = () => {
        if (!el) { // Ensure element exists before attempting to scroll
          stopAnimation();
          return;
        }

        // Apply scroll based on velocity
        el.scrollLeft += scrollVelocity.current;
        scrollVelocity.current *= 0.92; // Deceleration factor (slightly less aggressive)

        if (Math.abs(scrollVelocity.current) < 0.1) { // Stop animation if velocity is very low
          stopAnimation();
        } else {
          animationFrameId.current = requestAnimationFrame(animateScroll);
        }
      };
      
      const onWheel = (e) => {
        stopAnimation(); // Stop any ongoing animation
        if (e.deltaY === 0) return;
        e.preventDefault();
        el.scrollLeft += e.deltaY; // Direct scroll for immediate response
        scrollVelocity.current = e.deltaY * 0.5; // Apply a kick for momentum
        if (animationFrameId.current === null) {
          animationFrameId.current = requestAnimationFrame(animateScroll);
        }
      };

      const onMouseDown = (e) => {
        stopAnimation(); // Stop any ongoing animation
        isDown.current = true;
        el.classList.add('active');
        initialPageX.current = e.pageX;
        startX.current = e.pageX; // StartX now tracks actual pageX
        initialScrollLeft.current = el.scrollLeft;
        setIsDragging(false); // Reset dragging state on mouse down
        lastPosition.current = e.pageX;
        lastFrameTime.current = performance.now();
        scrollVelocity.current = 0; // Reset velocity on new drag

        animationFrameId.current = requestAnimationFrame(animateScroll); // Start animation for drag
      };

      const onMouseLeave = () => {
        if (isDown.current) {
          isDown.current = false; // Treat mouse leave as mouse up for dragging purposes
          el.classList.remove('active');
          el.classList.remove('dragging');
          // Momentum scroll will continue if scrollVelocity is non-zero
        }
      };

      const onMouseUp = () => {
        isDown.current = false;
        el.classList.remove('active');
        el.classList.remove('dragging');
        // Momentum scroll will continue if scrollVelocity is non-zero
      };

      const onMouseMove = (e) => {
        if (!isDown.current) return;
        e.preventDefault();

        const currentTime = performance.now();
        const deltaTime = currentTime - lastFrameTime.current;
        const currentX = e.pageX;
        const deltaX = currentX - lastPosition.current;
        lastPosition.current = currentX;
        lastFrameTime.current = currentTime;

        // Calculate drag distance from initial mousedown
        const dragDistance = Math.abs(currentX - initialPageX.current);

        if (dragDistance > dragThreshold) {
            setIsDragging(true);
        }
        
        // Update scroll position immediately during drag for responsiveness
        el.scrollLeft -= deltaX;

        // Calculate velocity based on actual scroll change over time
        if (deltaTime > 0) {
            scrollVelocity.current = -deltaX / (deltaTime / 16); // Scale to ~60fps frame
        }
        
        // Clamp max velocity
        if (Math.abs(scrollVelocity.current) > 50) {
            scrollVelocity.current = Math.sign(scrollVelocity.current) * 50;
        }
      };

      el.addEventListener('wheel', onWheel);
      el.addEventListener('mousedown', onMouseDown);
      el.addEventListener('mouseleave', onMouseLeave);
      el.addEventListener('mouseup', onMouseUp);
      el.addEventListener('mousemove', onMouseMove);

      return () => {
        stopAnimation();
        el.removeEventListener('wheel', onWheel);
        el.removeEventListener('mousedown', onMouseDown);
        el.removeEventListener('mouseleave', onMouseLeave);
        el.removeEventListener('mouseup', onMouseUp);
        el.removeEventListener('mousemove', onMouseMove);
      };
    }
  }, []);
  return { elRef, isDragging };
};
