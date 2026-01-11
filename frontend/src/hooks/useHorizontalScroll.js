import { useRef, useEffect, useState } from 'react';

export const useHorizontalScroll = () => {
  const elRef = useRef();
  const [isDragging, setIsDragging] = useState(false);
  const dragThreshold = 5; // Pixels to move before considering it a drag

  // Refs for animation and velocity
  const animationFrameId = useRef(null);
  const velocity = useRef(0);
  const lastScrollTime = useRef(0);
  const startX = useRef(0);
  const scrollLeft = useRef(0);
  const isDown = useRef(false);
  const initialPageX = useRef(0);

  useEffect(() => {
    const el = elRef.current;
    if (el) {
      const stopAnimation = () => {
        if (animationFrameId.current) {
          cancelAnimationFrame(animationFrameId.current);
          animationFrameId.current = null;
          velocity.current = 0;
        }
      };

      const animateScroll = () => {
        if (velocity.current !== 0) {
          el.scrollLeft += velocity.current;
          velocity.current *= 0.95; // Deceleration factor
          if (Math.abs(velocity.current) < 0.5) {
            velocity.current = 0;
          }
          animationFrameId.current = requestAnimationFrame(animateScroll);
        } else {
          stopAnimation();
        }
      };
      
      const onWheel = (e) => {
        stopAnimation(); // Stop any ongoing animation
        if (e.deltaY === 0) return;
        e.preventDefault();
        el.scrollLeft += e.deltaY;
        // Optionally add some gentle momentum to wheel scrolling
        velocity.current = e.deltaY * 0.5; // Initial velocity for wheel
        if (animationFrameId.current === null) {
          animationFrameId.current = requestAnimationFrame(animateScroll);
        }
      };

      const onMouseDown = (e) => {
        stopAnimation(); // Stop any ongoing animation
        isDown.current = true;
        el.classList.add('active');
        initialPageX.current = e.pageX;
        startX.current = e.pageX - el.offsetLeft;
        scrollLeft.current = el.scrollLeft;
        setIsDragging(false); // Reset dragging state on mouse down
        lastScrollTime.current = performance.now();
        velocity.current = 0; // Reset velocity on new drag
      };

      const onMouseLeave = () => {
        if (isDown.current) {
          // If mouse leaves while dragging, initiate momentum scroll
          animationFrameId.current = requestAnimationFrame(animateScroll);
        }
        isDown.current = false;
        el.classList.remove('active');
        el.classList.remove('dragging');
      };

      const onMouseUp = () => {
        isDown.current = false;
        el.classList.remove('active');
        el.classList.remove('dragging');
        // Initiate momentum scroll after releasing mouse
        animationFrameId.current = requestAnimationFrame(animateScroll);
      };

      const onMouseMove = (e) => {
        if (!isDown.current) return;
        e.preventDefault();
        const currentTime = performance.now();
        const deltaTime = currentTime - lastScrollTime.current;
        lastScrollTime.current = currentTime;

        const x = e.pageX - el.offsetLeft;
        const walk = (x - startX.current); // Raw pixel movement

        // Only set isDragging to true if movement exceeds threshold
        if (Math.abs(e.pageX - initialPageX.current) > dragThreshold) {
            setIsDragging(true);
        }
        
        const newScrollLeft = scrollLeft.current - walk;
        const scrollDelta = newScrollLeft - el.scrollLeft;
        velocity.current = scrollDelta / (deltaTime / 1000); // pixels per second
        if (Math.abs(velocity.current) > 200) { // Limit max velocity
            velocity.current = Math.sign(velocity.current) * 200;
        }

        el.scrollLeft = newScrollLeft;
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
